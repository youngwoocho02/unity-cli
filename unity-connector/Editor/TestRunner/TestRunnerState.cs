using System;
using System.Collections.Generic;
using System.IO;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEditor.TestTools.TestRunner.Api;
using UnityEngine;
using Object = UnityEngine.Object;

namespace UnityCliConnector.TestRunner
{
    /// <summary>
    /// Survives domain reloads via [InitializeOnLoad].
    /// If a PlayMode test run was in progress when the domain reloaded,
    /// re-registers callbacks so RunFinished still fires and writes results.
    /// </summary>
    [InitializeOnLoad]
    public static class TestRunnerState
    {
        static readonly string s_StatusDir = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".unity-cli", "status");

        static TestRunnerState()
        {
            AssemblyReloadEvents.afterAssemblyReload += OnAfterAssemblyReload;
        }

        /// <summary>
        /// Called by RunTests before executing a PlayMode run.
        /// Writes a pending file so we can re-attach after domain reload.
        /// </summary>
        public static void MarkPending(int port, string filter)
        {
            var pending = new { port, filter = filter ?? "" };
            try
            {
                Directory.CreateDirectory(s_StatusDir);
                File.WriteAllText(PendingFilePath(port), JsonConvert.SerializeObject(pending));
            }
            catch { }
        }

        public static void ClearPending(int port)
        {
            try
            {
                var path = PendingFilePath(port);
                if (File.Exists(path)) File.Delete(path);
            }
            catch { }
        }

        static void OnAfterAssemblyReload()
        {
            // Find any pending file — port is encoded in filename
            try
            {
                Directory.CreateDirectory(s_StatusDir);
                foreach (var file in Directory.GetFiles(s_StatusDir, "test-pending-*.json"))
                {
                    var json = File.ReadAllText(file);
                    var pending = JObject.Parse(json);
                    var port   = pending["port"]?.Value<int>() ?? 0;
                    var filter = pending["filter"]?.Value<string>();

                    if (port == 0) continue;

                    // Only re-attach if this is our port
                    if (port != HttpServer.Port) continue;

                    ReattachCallbacks(port, filter);
                }
            }
            catch { }
        }

        static void ReattachCallbacks(int port, string filter)
        {
            var passed  = new List<string>();
            var failed  = new List<string>();
            var skipped = new List<string>();

            var api = ScriptableObject.CreateInstance<TestRunnerApi>();

            var callbacks = new Callbacks(
                onTestResult: result =>
                {
                    if (result.Test.IsSuite) return;
                    CollectResult(result, passed, failed, skipped);
                },
                onRunFinished: _ =>
                {
                    Object.DestroyImmediate(api);
                    ClearPending(port);
                    RunTests.WriteResultsFile(port, passed, failed, skipped);
                }
            );

            api.RegisterCallbacks(callbacks);
            // Don't call Execute again — the run is already in progress,
            // we're just re-registering to receive the RunFinished callback.
        }

        static void CollectResult(ITestResultAdaptor result,
            List<string> passed, List<string> failed, List<string> skipped)
        {
            var name   = result.Test.FullName;
            var status = result.ResultState.ToString();
            switch (status)
            {
                case "Passed":  passed.Add(name); break;
                case "Failed":
                case "Error":   failed.Add($"{name}: {result.Message}"); break;
                default:        skipped.Add(name); break;
            }
        }

        static string PendingFilePath(int port) =>
            Path.Combine(s_StatusDir, $"test-pending-{port}.json");

        private class Callbacks : ICallbacks
        {
            private readonly Action<ITestResultAdaptor> _onTestResult;
            private readonly Action<ITestResultAdaptor> _onRunFinished;

            public Callbacks(Action<ITestResultAdaptor> onTestResult, Action<ITestResultAdaptor> onRunFinished)
            {
                _onTestResult  = onTestResult;
                _onRunFinished = onRunFinished;
            }

            public void RunStarted(ITestAdaptor testsToRun) { }
            public void RunFinished(ITestResultAdaptor result) => _onRunFinished(result);
            public void TestStarted(ITestAdaptor test) { }
            public void TestFinished(ITestResultAdaptor result) => _onTestResult(result);
        }
    }
}
