using System;
using System.Collections.Generic;
using System.IO;
using System.Threading.Tasks;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;
using UnityEditor.TestTools.TestRunner.Api;
using UnityEngine;
using Object = UnityEngine.Object;

namespace UnityCliConnector.TestRunner
{
    [UnityCliTool(Description = "Run Unity EditMode or PlayMode tests and return results.")]
    public static class RunTests
    {
        static readonly string s_StatusDir = Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".unity-cli", "status");

        public class Parameters
        {
            [ToolParameter("Test mode to run: EditMode or PlayMode", Required = true)]
            public string Mode { get; set; }

            [ToolParameter("Optional test filter: namespace, class, or full test name")]
            public string Filter { get; set; }
        }

        public static Task<object> HandleCommand(JObject @params)
        {
            if (@params == null)
                return Task.FromResult<object>(new ErrorResponse("Parameters cannot be null."));

            var p = new ToolParams(@params);

            var modeResult = p.GetRequired("mode");
            if (!modeResult.IsSuccess)
                return Task.FromResult<object>(new ErrorResponse(modeResult.ErrorMessage));

            var modeStr = modeResult.Value.Trim();
            TestMode testMode;
            if (modeStr.Equals("EditMode", StringComparison.OrdinalIgnoreCase))
                testMode = TestMode.EditMode;
            else if (modeStr.Equals("PlayMode", StringComparison.OrdinalIgnoreCase))
                testMode = TestMode.PlayMode;
            else
                return Task.FromResult<object>(new ErrorResponse($"Unknown mode '{modeStr}'. Use EditMode or PlayMode."));

            var filter = p.Get("filter", null);

            // EditMode: no domain reload, hold connection and return results directly
            if (testMode == TestMode.EditMode)
                return ExecuteInProcess(testMode, filter);

            // PlayMode: domain reload will kill the connection.
            // Kick off the run, return immediately, Go polls the results file.
            StartPlayModeRun(filter);
            return Task.FromResult<object>(new SuccessResponse("running", new { port = HttpServer.Port }));
        }

        // --- EditMode path: hold connection open, return results when done ---

        private static Task<object> ExecuteInProcess(TestMode mode, string filter)
        {
            var tcs = new TaskCompletionSource<object>(TaskCreationOptions.RunContinuationsAsynchronously);
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
                    if (tcs.Task.IsCompleted) return;
                    Object.DestroyImmediate(api);
                    tcs.TrySetResult(BuildResponse(passed, failed, skipped));
                }
            );

            api.RegisterCallbacks(callbacks);
            api.Execute(new ExecutionSettings(BuildFilter(mode, filter)));
            return tcs.Task;
        }

        // --- PlayMode path: fire and forget, write results to file after domain reload ---

        private static void StartPlayModeRun(string filter)
        {
            var passed  = new List<string>();
            var failed  = new List<string>();
            var skipped = new List<string>();
            var port    = HttpServer.Port;

            // Clear any stale results file
            var resultsPath = ResultsFilePath(port);
            try { if (File.Exists(resultsPath)) File.Delete(resultsPath); } catch { }

            // Write pending file so TestRunnerState can re-attach callbacks after domain reload
            TestRunnerState.MarkPending(port, filter);

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
                    TestRunnerState.ClearPending(port);
                    WriteResultsFile(port, passed, failed, skipped);
                }
            );

            api.RegisterCallbacks(callbacks);
            api.Execute(new ExecutionSettings(BuildFilter(TestMode.PlayMode, filter)));
        }

        internal static void WriteResultsFile(int port, List<string> passed, List<string> failed, List<string> skipped)
        {
            var result = new
            {
                success  = failed.Count == 0,
                message  = failed.Count > 0 ? $"{failed.Count} test(s) failed." : $"All {passed.Count} test(s) passed.",
                data     = new
                {
                    total    = passed.Count + failed.Count + skipped.Count,
                    passed   = passed.Count,
                    failed   = failed.Count,
                    skipped  = skipped.Count,
                    failures = failed,
                    passes   = passed,
                }
            };

            try
            {
                Directory.CreateDirectory(s_StatusDir);
                File.WriteAllText(ResultsFilePath(port), JsonConvert.SerializeObject(result));
            }
            catch { }
        }

        private static string ResultsFilePath(int port) =>
            Path.Combine(s_StatusDir, $"test-results-{port}.json");

        // --- Shared helpers ---

        private static void CollectResult(ITestResultAdaptor result,
            List<string> passed, List<string> failed, List<string> skipped)
        {
            var name   = result.Test.FullName;
            var status = result.ResultState.ToString();
            switch (status)
            {
                case "Passed":           passed.Add(name);  break;
                case "Failed":
                case "Error":            failed.Add($"{name}: {result.Message}"); break;
                default:                 skipped.Add(name); break;
            }
        }

        private static object BuildResponse(List<string> passed, List<string> failed, List<string> skipped)
        {
            var summary = new
            {
                total    = passed.Count + failed.Count + skipped.Count,
                passed   = passed.Count,
                failed   = failed.Count,
                skipped  = skipped.Count,
                failures = failed,
                passes   = passed,
            };
            return failed.Count > 0
                ? (object)new ErrorResponse($"{failed.Count} test(s) failed.", summary)
                : new SuccessResponse($"All {passed.Count} test(s) passed.", summary);
        }

        private static Filter BuildFilter(TestMode mode, string filterStr)
        {
            var f = new Filter { testMode = mode };
            if (!string.IsNullOrEmpty(filterStr))
            {
                f.testNames  = new[] { filterStr };
                f.groupNames = new[] { filterStr };
            }
            return f;
        }

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
