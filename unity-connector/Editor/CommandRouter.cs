using System;
using System.Threading;
using System.Threading.Tasks;
using Newtonsoft.Json.Linq;
using UnityEngine;

namespace UnityCliConnector
{
    /// <summary>
    /// Routes incoming command requests to the appropriate tool handler.
    /// All requests are serialized through a single queue to prevent
    /// race conditions when multiple CLI agents access the same Unity instance.
    /// </summary>
    public static class CommandRouter
    {
        static readonly SemaphoreSlim s_Lock = new(1, 1);

        /// <summary>
        /// 핸들러를 하나씩만 실행한다. SemaphoreSlim(1,1)이 직렬화한다.
        /// </summary>
        public static async Task<object> Dispatch(string command, JObject parameters)
        {
            await s_Lock.WaitAsync();
            try
            {
                return await DispatchInternal(command, parameters);
            }
            finally
            {
                s_Lock.Release();
            }
        }

        static async Task<object> DispatchInternal(string command, JObject parameters)
        {
            if (command == "list")
                return new SuccessResponse("Available tools", ToolDiscovery.GetToolSchemas());

            var handler = ToolDiscovery.FindHandler(command);
            if (handler == null)
                return new ErrorResponse($"Unknown command: {command}");

            try
            {
                var result = handler.Invoke(null, new object[] { parameters ?? new JObject() });

                // HandleCommand가 Task<object>면 끝날 때까지 기다린다 (테스트 러너 등).
                if (result is Task<object> asyncTask)
                    return await asyncTask;

                if (result is Task task)
                {
                    await task;
                    return new SuccessResponse($"{command} completed");
                }

                return result ?? new SuccessResponse($"{command} completed");
            }
            catch (Exception ex)
            {
                var inner = ex.InnerException ?? ex;
                Debug.LogException(inner);
                return new ErrorResponse($"{command} failed: {inner.Message}");
            }
        }
    }
}
