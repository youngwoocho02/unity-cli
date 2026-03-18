using System;
using System.IO;
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
            Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".unity-cli", "status");

        static double s_LastWrite;
        const double INTERVAL = 0.5;
        static string s_ForcedState;
        static double s_CompileRequestTime;

        static Heartbeat()
        {
            EditorApplication.update += Tick;
            EditorApplication.quitting += Cleanup;
            AssemblyReloadEvents.beforeAssemblyReload += OnBeforeAssemblyReload;
            AssemblyReloadEvents.afterAssemblyReload += () => { s_ForcedState = null; s_LastWrite = 0; };
            CompilationPipeline.compilationStarted += _ => WriteState("compiling");
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
                var path = Path.Combine(s_Dir, $"{HttpServer.Port}.json");
                File.WriteAllText(path, JsonConvert.SerializeObject(status));

                // Keep instances.json in sync so CLI always finds the correct port
                InstanceRegistry.Register(HttpServer.Port, HttpServer.AuthToken);
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

        public static void Cleanup()
        {
            if (HttpServer.Port == 0) return;

            try
            {
                var path = Path.Combine(s_Dir, $"{HttpServer.Port}.json");
                if (File.Exists(path)) File.Delete(path);
            }
            catch
            {
            }
        }
    }
}
