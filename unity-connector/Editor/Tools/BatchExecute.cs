using System.Collections.Generic;
using System.Threading.Tasks;
using Newtonsoft.Json.Linq;
using UnityEditor;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Name = "batch", Description = "Execute multiple commands in a single request. Params: commands (JSON array of {command, params}), stop_on_error (bool).")]
    public static class BatchExecute
    {
        public class Parameters
        {
            [ToolParameter("JSON array of commands: [{\"command\":\"...\",\"params\":{...}}, ...]", Required = true)]
            public string Commands { get; set; }

            [ToolParameter("Stop processing after the first failure (default false)")]
            public bool StopOnError { get; set; }
        }

        public static async Task<object> HandleCommand(JObject @params)
        {
            if (@params == null)
                return new ErrorResponse("Parameters cannot be null.");

            var p = new ToolParams(@params);
            var commandsToken = p.GetRaw("commands");
            if (commandsToken == null || commandsToken.Type != JTokenType.Array)
                return new ErrorResponse("'commands' must be a JSON array of {command, params} objects.");

            var commandsArray = (JArray)commandsToken;
            if (commandsArray.Count == 0)
                return new ErrorResponse("'commands' array is empty.");

            bool stopOnError = p.GetBool("stop_on_error");

            // 하나의 Undo 그룹으로 묶기
            Undo.IncrementCurrentGroup();
            int undoGroup = Undo.GetCurrentGroup();
            Undo.SetCurrentGroupName("Batch Execute");

            var results = new List<object>();
            int succeeded = 0;
            int failed = 0;

            foreach (var item in commandsArray)
            {
                if (item.Type != JTokenType.Object)
                {
                    results.Add(new { command = "(invalid)", success = false, message = "Entry must be {command, params}." });
                    failed++;
                    if (stopOnError) break;
                    continue;
                }

                var entry = (JObject)item;
                string command = entry.Value<string>("command");
                var entryParams = entry.Value<JObject>("params");

                if (string.IsNullOrEmpty(command))
                {
                    results.Add(new { command = "(missing)", success = false, message = "'command' field is required." });
                    failed++;
                    if (stopOnError) break;
                    continue;
                }

                // batch 자기 자신 호출 방지
                if (command == "batch")
                {
                    results.Add(new { command, success = false, message = "Recursive batch is not allowed." });
                    failed++;
                    if (stopOnError) break;
                    continue;
                }

                // CommandRouter.Dispatch를 직접 호출하면 SemaphoreSlim 데드락 발생
                // → DispatchInternal 대신 ToolDiscovery로 직접 핸들러 호출
                var handler = ToolDiscovery.FindHandler(command);
                if (handler == null)
                {
                    results.Add(new { command, success = false, message = $"Unknown command: {command}" });
                    failed++;
                    if (stopOnError) break;
                    continue;
                }

                try
                {
                    var result = handler.Invoke(null, new object[] { entryParams ?? new JObject() });

                    object resolved;
                    if (result is Task<object> asyncTask)
                        resolved = await asyncTask;
                    else if (result is Task task)
                    {
                        await task;
                        resolved = new SuccessResponse($"{command} completed");
                    }
                    else
                        resolved = result ?? new SuccessResponse($"{command} completed");

                    // SuccessResponse/ErrorResponse 판별
                    bool isSuccess = true;
                    string message = "";
                    object data = null;

                    if (resolved is SuccessResponse sr)
                    {
                        message = sr.message;
                        data = sr.data;
                    }
                    else if (resolved is ErrorResponse er)
                    {
                        isSuccess = false;
                        message = er.message;
                        data = er.data;
                    }

                    results.Add(new { command, success = isSuccess, message, data });

                    if (isSuccess)
                        succeeded++;
                    else
                    {
                        failed++;
                        if (stopOnError) break;
                    }
                }
                catch (System.Exception ex)
                {
                    var inner = ex.InnerException ?? ex;
                    results.Add(new { command, success = false, message = inner.Message });
                    failed++;
                    if (stopOnError) break;
                }
            }

            Undo.CollapseUndoOperations(undoGroup);

            return new SuccessResponse($"Batch: {succeeded} succeeded, {failed} failed.", new
            {
                total = commandsArray.Count,
                succeeded,
                failed,
                results
            });
        }
    }
}
