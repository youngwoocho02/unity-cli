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
        static SemaphoreSlim s_Lock = new(1, 1);

        public static async Task<object> Dispatch(string command, JObject parameters)
        {
            // Capture locally so a concurrent ResetLock() swap doesn't make us release a
            // semaphore we never acquired. A still-running orphaned call releases the old
            // semaphore (now unreferenced) instead of double-releasing the new one.
            var sem = s_Lock;
            await sem.WaitAsync();
            try
            {
                return await DispatchInternal(command, parameters);
            }
            finally
            {
                sem.Release();
            }
        }

        /// <summary>
        /// Replaces the dispatch semaphore with a fresh one so new commands can run
        /// even if a previous handler is still hung holding the old semaphore.
        /// </summary>
        public static void ResetLock()
        {
            s_Lock = new SemaphoreSlim(1, 1);
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
