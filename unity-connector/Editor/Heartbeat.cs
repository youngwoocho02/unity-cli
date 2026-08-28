using System;
using System.IO;
using System.Security.Cryptography;
using System.Text;
using Newtonsoft.Json;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector
{
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
            }
        }

        static string GetConnectorVersion()
        {
            return CONNECTOR_VERSION;
        }

        public static string LastState => s_LastState ?? "starting";

        public static bool IsDispatchableState()
        {
            switch (LastState)
            {
                case "ready":
                case "playing":
                case "paused":
                    return s_LastTimestamp > 0 && s_LastPid > 0 && !string.IsNullOrEmpty(s_LastProjectPath);
                default:
                    return false;
            }
        }

        public static object HealthSnapshot()
        {
            var ready = s_LastTimestamp > 0 && !string.IsNullOrEmpty(s_LastProjectPath) && s_LastPid > 0;
            return new
            {
                state = LastState,
                projectPath = s_LastProjectPath ?? "",
                port = HttpServer.Port,
                pid = s_LastPid,
                unityVersion = s_LastUnityVersion ?? "",
                connectorVersion = GetConnectorVersion(),
                timestamp = s_LastTimestamp,
                compileErrors = s_LastCompileErrors,
                listenerRunning = HttpServer.IsRunning,
                ready,
                dispatchable = ready && HttpServer.CanAcceptCommand(),
            };
        }

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

        public static void MarkStopped()
        {
            s_ForcedState = "stopped";
            Write();
        }
    }
}
