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
        const string CONNECTOR_VERSION = "0.3.16";
        static string s_ForcedState;
        static double s_CompileRequestTime;
        static string s_FilePath;

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
            if (HttpServer.Port == 0) return;

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
            var status = new
            {
                state = s_ForcedState ?? GetState(),
                projectPath = Application.dataPath.Replace("/Assets", ""),
                port = HttpServer.Port,
                pid = System.Diagnostics.Process.GetCurrentProcess().Id,
                unityVersion = Application.unityVersion,
                connectorVersion = GetConnectorVersion(),
                timestamp = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds(),
                compileErrors = EditorUtility.scriptCompilationFailed,
            };

            try
            {
                Directory.CreateDirectory(s_Dir);
                File.WriteAllText(GetFilePath(), JsonConvert.SerializeObject(status));
            }
            catch
            {
            }
        }

        static string GetConnectorVersion()
        {
            return CONNECTOR_VERSION;
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
            if (HttpServer.Port == 0) return;
            s_ForcedState = "stopped";
            Write();
        }
    }
}
