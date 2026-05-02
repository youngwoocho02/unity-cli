using System;
using System.Collections.Generic;
using System.IO;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector
{
    /// <summary>
    /// Editor window that surfaces the connector's runtime state and recovery
    /// levers (Start / Stop / Restart / Purge) without needing the CLI.
    /// </summary>
    public class ConnectorStatusWindow : EditorWindow
    {
        // Mirrors RunTests.StatusDir. TestRunner is in a separate asmdef that depends
        // on this one, so we can't reference it back here — duplicate the path.
        static readonly string s_StatusDir = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".unity-cli", "status");

        struct PendingTest
        {
            public int Port;
            public string Filter;
            public string Path;
        }

        [MenuItem("Tools/Unity CLI/Server Status")]
        static void Open() => GetWindow<ConnectorStatusWindow>("Unity CLI Server");

        void OnEnable() => EditorApplication.update += Repaint;
        void OnDisable() => EditorApplication.update -= Repaint;

        void OnGUI()
        {
            var running = HttpServer.IsRunning;
            var state = Heartbeat.CurrentState;

            EditorGUILayout.Space(6);

            EditorGUILayout.BeginHorizontal();
            GUILayout.Label("Status", GUILayout.Width(60));
            var prev = GUI.color;
            GUI.color = running ? Color.green : Color.gray;
            GUILayout.Label(running ? "●" : "○", GUILayout.Width(18));
            GUI.color = prev;
            GUILayout.Label(running ? "Running" : "Stopped");
            EditorGUILayout.EndHorizontal();

            if (running)
            {
                EditorGUILayout.BeginHorizontal();
                GUILayout.Label("Port", GUILayout.Width(60));
                GUILayout.Label(HttpServer.Port.ToString());
                EditorGUILayout.EndHorizontal();

                EditorGUILayout.BeginHorizontal();
                GUILayout.Label("State", GUILayout.Width(60));
                GUILayout.Label(state);
                EditorGUILayout.EndHorizontal();
            }

            var pendingCommands = HttpServer.PendingCount;
            var pendingTests = ListPendingTests();

            EditorGUILayout.Space(8);
            EditorGUILayout.BeginHorizontal();
            GUILayout.Label("Queued", GUILayout.Width(60));
            GUILayout.Label(pendingCommands.ToString());
            EditorGUILayout.EndHorizontal();

            EditorGUILayout.BeginHorizontal();
            GUILayout.Label("Pending tests", GUILayout.Width(90));
            GUILayout.Label(pendingTests.Count.ToString());
            EditorGUILayout.EndHorizontal();

            foreach (var entry in pendingTests)
            {
                var detail = string.IsNullOrEmpty(entry.Filter)
                    ? $"port {entry.Port}"
                    : $"port {entry.Port} — {entry.Filter}";
                EditorGUILayout.LabelField("    " + detail);
            }

            EditorGUILayout.Space(8);
            EditorGUILayout.BeginHorizontal();

            if (running)
            {
                if (GUILayout.Button("Stop"))
                    HttpServer.ManualStop();
                if (GUILayout.Button("Restart"))
                {
                    HttpServer.ManualStop();
                    HttpServer.ManualStart();
                }
            }
            else
            {
                if (GUILayout.Button("Start"))
                    HttpServer.ManualStart();
            }

            using (new EditorGUI.DisabledScope(pendingCommands == 0 && pendingTests.Count == 0))
            {
                if (GUILayout.Button("Purge"))
                {
                    var confirmed = EditorUtility.DisplayDialog(
                        "Purge pending work",
                        $"Fault {pendingCommands} queued command(s) and delete {pendingTests.Count} pending test file(s)?\n\n" +
                        "Any CLI client currently waiting on a response will see an error.",
                        "Purge",
                        "Cancel");
                    if (confirmed)
                    {
                        var faulted = HttpServer.PurgePending();
                        var deleted = PurgePendingTests(pendingTests);
                        Debug.Log($"[UnityCliConnector] Purged {faulted} pending command(s), {deleted} pending test file(s)");
                    }
                }
            }

            EditorGUILayout.EndHorizontal();
            EditorGUILayout.Space(4);
        }

        static List<PendingTest> ListPendingTests()
        {
            var result = new List<PendingTest>();
            try
            {
                if (!Directory.Exists(s_StatusDir)) return result;
                foreach (var file in Directory.GetFiles(s_StatusDir, "test-pending-*.json"))
                {
                    try
                    {
                        var json = JObject.Parse(File.ReadAllText(file));
                        result.Add(new PendingTest
                        {
                            Port   = json["port"]?.Value<int>() ?? 0,
                            Filter = json["filter"]?.Value<string>() ?? "",
                            Path   = file,
                        });
                    }
                    catch { }
                }
            }
            catch { }
            return result;
        }

        static int PurgePendingTests(IEnumerable<PendingTest> entries)
        {
            var deleted = 0;
            foreach (var e in entries)
            {
                try { File.Delete(e.Path); deleted++; } catch { }
            }
            return deleted;
        }
    }
}
