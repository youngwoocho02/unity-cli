using System;
using System.IO;
using System.Security.Cryptography;
using System.Text;
using Newtonsoft.Json;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector
{
    /// <summary>
    /// ~/.unity-cli/instances/에 0.5초마다 상태를 쓴다.
    /// CLI는 이 파일로 Unity를 찾고, /health는 마지막 스냅샷을 그대로 돌려준다.
    /// compiling/reloading은 명령을 받지 않는 상태로 취급한다.
    /// </summary>
    [InitializeOnLoad]
    public static class Heartbeat
    {
        static readonly string s_Dir = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".unity-cli", "instances");

        static double s_LastWrite;
        const double INTERVAL = 0.5;
        const string CONNECTOR_VERSION = "0.3.22";
        static string s_ForcedState;
        static double s_CompileRequestTime;
        static string s_FilePath;
        static string s_LastState;
        static string s_LastProjectPath;
        static string s_LastUnityVersion;
        static int s_LastPid;
        static long s_LastTimestamp;
        static bool s_LastCompileErrors;

        static Heartbeat()
        {
            // 에디터 틱마다 쓰고, 종료/리로드/플레이 전환 때 상태를 강제한다.
            EditorApplication.update += Tick;
            EditorApplication.quitting += Cleanup;
            AssemblyReloadEvents.beforeAssemblyReload += OnBeforeAssemblyReload;
            AssemblyReloadEvents.afterAssemblyReload += () => { s_ForcedState = null; s_LastWrite = 0; };
            EditorApplication.playModeStateChanged += OnPlayModeChanged;
        }

        static void OnBeforeAssemblyReload()
        {
            WriteState("reloading");
        }

        static void OnPlayModeChanged(PlayModeStateChange change)
        {
            // play 진입 직전은 isPlaying이 아직 false라 GetState()가 ready로 나올 수 있다.
            if (change == PlayModeStateChange.ExitingEditMode)
                WriteState("entering_playmode");
        }

        static void WriteState(string state)
        {
            s_ForcedState = state;
            Write();
        }

        /// <summary>
        /// Marks that a compile was requested. Keeps "compiling" state forced
        /// for a grace period so the CLI poller never sees a premature "ready".
        /// </summary>
        public static void MarkCompileRequested()
        {
            s_CompileRequestTime = EditorApplication.timeSinceStartup;
            WriteState("compiling");
        }

        static void Tick()
        {
            if (!HttpServer.IsRunning) return;

            var now = EditorApplication.timeSinceStartup;
            if (now - s_LastWrite < INTERVAL) return;
            s_LastWrite = now;

            // RequestScriptCompilation 직후 isCompiling이 잠깐 false일 수 있다.
            // 3초 동안은 compiling을 유지해 CLI가 성급히 명령을 보내지 않게 한다.
            if (s_CompileRequestTime > 0)
            {
                if (now - s_CompileRequestTime < 3.0 && EditorApplication.isCompiling == false)
                {
                    Write();
                    return;
                }
                s_CompileRequestTime = 0;
            }

            s_ForcedState = null;
            Write();
        }

        // 프로젝트 경로 해시로 파일 이름을 고정한다. 같은 프로젝트는 항상 같은 파일.
        static string GetFilePath()
        {
            if (s_FilePath != null) return s_FilePath;
            var projectPath = Application.dataPath.Replace("/Assets", "");
            using var md5 = MD5.Create();
            var hash = BitConverter.ToString(md5.ComputeHash(Encoding.UTF8.GetBytes(projectPath)))
                .Replace("-", "").Substring(0, 16).ToLower();
            s_FilePath = Path.Combine(s_Dir, $"{hash}.json");
            return s_FilePath;
        }

        // tmp에 쓴 뒤 Replace/Move로 교체한다. CLI가 반쯤 쓰인 JSON을 덜 읽게 한다.
        static void Write()
        {
            var state = s_ForcedState ?? GetState();
            var projectPath = Application.dataPath.Replace("/Assets", "");
            var pid = System.Diagnostics.Process.GetCurrentProcess().Id;
            var timestamp = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
            var compileErrors = EditorUtility.scriptCompilationFailed;

            s_LastState = state;
            s_LastProjectPath = projectPath;
            s_LastUnityVersion = Application.unityVersion;
            s_LastPid = pid;
            s_LastTimestamp = timestamp;
            s_LastCompileErrors = compileErrors;

            var status = new
            {
                state,
                projectPath,
                port = HttpServer.Port,
                pid,
                unityVersion = s_LastUnityVersion,
                connectorVersion = GetConnectorVersion(),
                timestamp,
                compileErrors,
            };

            try
            {
                Directory.CreateDirectory(s_Dir);
                var path = GetFilePath();
                var tmp = path + ".tmp";
                File.WriteAllText(tmp, JsonConvert.SerializeObject(status));
                if (File.Exists(path))
                    File.Replace(tmp, path, null);
                else
                    File.Move(tmp, path);
            }
            catch
            {
                // 다른 프로세스가 파일을 잡고 있어도 에디터를 멈추지 않는다.
            }
        }

        static string GetConnectorVersion()
        {
            return CONNECTOR_VERSION;
        }

        public static string CurrentState => s_LastState ?? "starting";

        /// <summary>
        /// ready / playing / paused 이고 heartbeat 필드가 채워졌을 때만 명령을 받는다.
        /// </summary>
        public static bool CanRunCommands()
        {
            switch (CurrentState)
            {
                case "ready":
                case "playing":
                case "paused":
                    return s_LastTimestamp > 0 && s_LastPid > 0 && !string.IsNullOrEmpty(s_LastProjectPath);
                default:
                    return false;
            }
        }

        /// <summary>
        /// GET /health 본문. 메인 스레드 디스패치 없이 마지막 Write() 값을 그대로 준다.
        /// </summary>
        public static object HealthSnapshot()
        {
            var ready = s_LastTimestamp > 0 && !string.IsNullOrEmpty(s_LastProjectPath) && s_LastPid > 0;
            return new
            {
                state = CurrentState,
                projectPath = s_LastProjectPath ?? "",
                port = HttpServer.Port,
                pid = s_LastPid,
                unityVersion = s_LastUnityVersion ?? "",
                connectorVersion = GetConnectorVersion(),
                timestamp = s_LastTimestamp,
                compileErrors = s_LastCompileErrors,
                listenerRunning = HttpServer.IsRunning,
                ready,
            };
        }

        // Unity API로 현재 상태를 읽는다. 강제 상태(s_ForcedState)가 없을 때만 쓴다.
        static string GetState()
        {
            if (EditorApplication.isCompiling) return "compiling";
            if (EditorApplication.isUpdating) return "refreshing";
            if (EditorApplication.isPlaying)
                return EditorApplication.isPaused ? "paused" : "playing";
            return "ready";
        }

        public static void Cleanup()
        {
            MarkStopped();
        }

        /// <summary>
        /// 에디터 종료 또는 리스너 실패 때 stopped를 써서 CLI가 죽은 인스턴스로 보게 한다.
        /// </summary>
        public static void MarkStopped()
        {
            s_ForcedState = "stopped";
            Write();
        }
    }
}
