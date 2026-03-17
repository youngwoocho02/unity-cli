using System;
using System.IO;
using System.Security.Cryptography;
using System.Text;
using Newtonsoft.Json;
using UnityEditor;
using UnityEditor.Compilation;
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
        static string s_ForcedState;
        static double s_CompileRequestTime;

        static Heartbeat()
        {
            EditorApplication.update += Tick;
            EditorApplication.quitting += OnQuitting;
            AssemblyReloadEvents.beforeAssemblyReload += OnBeforeAssemblyReload;
            AssemblyReloadEvents.afterAssemblyReload += () => { s_ForcedState = null; s_LastWrite = 0; };
            CompilationPipeline.compilationStarted += _ => WriteState("compiling");
            EditorApplication.playModeStateChanged += OnPlayModeChanged;
        }

        static void OnBeforeAssemblyReload()
        {
            WriteState("reloading");
        }

        static void OnQuitting()
        {
            WriteState("stopped");
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
                if (EditorApplication.isCompiling)
                {
                    // Compilation started, hand off to normal GetState() flow
                    s_CompileRequestTime = 0;
                }
                else if (now - s_CompileRequestTime < 10.0)
                {
                    // Still waiting for compilation to start, keep forced "compiling" state
                    Write();
                    return;
                }
                else
                {
                    // Grace period expired, compilation never started
                    s_CompileRequestTime = 0;
                }
            }

            s_ForcedState = null;
            Write();
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
                timestamp = DateTimeOffset.UtcNow.ToUnixTimeMilliseconds(),
            };

            try
            {
                Directory.CreateDirectory(s_Dir);
                var path = Path.Combine(s_Dir, $"{ProjectHash()}.json");
                File.WriteAllText(path, JsonConvert.SerializeObject(status));
            }
            catch
            {
            }
        }

        static string GetState()
        {
            if (EditorApplication.isCompiling) return "compiling";
            if (EditorApplication.isUpdating) return "refreshing";
            if (EditorApplication.isPlaying)
                return EditorApplication.isPaused ? "paused" : "playing";
            return "ready";
        }

        static string s_ProjectHash;

        static string ProjectHash()
        {
            if (s_ProjectHash != null) return s_ProjectHash;
            var projectPath = Application.dataPath.Replace("/Assets", "");
            using var md5 = MD5.Create();
            var bytes = md5.ComputeHash(Encoding.UTF8.GetBytes(projectPath));
            s_ProjectHash = BitConverter.ToString(bytes).Replace("-", "").Substring(0, 16).ToLowerInvariant();
            return s_ProjectHash;
        }
    }
}
