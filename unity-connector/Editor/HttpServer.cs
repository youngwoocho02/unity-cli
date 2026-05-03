using System;
using System.Collections.Concurrent;
using System.IO;
using System.Net;
using System.Text;
using System.Threading;
using System.Threading.Tasks;
using Newtonsoft.Json;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector
{
    /// <summary>
    /// Lightweight HTTP server on localhost. Receives CLI commands as POST /command,
    /// dispatches via CommandRouter, returns JSON responses.
    /// Uses ConcurrentQueue + EditorApplication.update for main-thread marshaling.
    /// Background editor throttling can still delay command execution.
    /// Survives domain reloads via InitializeOnLoad.
    /// </summary>
    [InitializeOnLoad]
    public static class HttpServer
    {
        const int DEFAULT_PORT = 8090;
        const int MAX_PORT_ATTEMPTS = 10;
        const double AUTO_RESTART_INTERVAL = 1.0;
        const double FAILURE_LOG_INTERVAL = 5.0;

        static HttpListener s_Listener;
        static CancellationTokenSource s_Cts;
        static int s_Port;
        static double s_NextStartAttemptTime;
        static string s_LastFailureMessage;
        static double s_LastFailureLogTime;

        static readonly ConcurrentQueue<WorkItem> s_Queue = new();

        struct WorkItem
        {
            public string Command;
            public JObject Parameters;
            public TaskCompletionSource<object> Tcs;
        }

        static HttpServer()
        {
            Start();
            EditorApplication.quitting += Stop;
            AssemblyReloadEvents.beforeAssemblyReload += StopListener;
            AssemblyReloadEvents.afterAssemblyReload += Start;
            EditorApplication.update += ProcessQueue;
        }

        public static int Port => s_Port;
        public static bool IsRunning => s_Listener != null && s_Listener.IsListening;

        static void Start()
        {
            if (IsRunning) return;
            // Stale listener left behind by a crashed ListenLoop — tear it down before rebinding.
            if (s_Listener != null) StopListener();

            for (var attempt = 0; attempt < MAX_PORT_ATTEMPTS; attempt++)
            {
                var port = DEFAULT_PORT + attempt;
                if (TryStartOnPort(port))
                    return;
            }

            Heartbeat.MarkStopped();
            ScheduleRetry();
            LogStartFailure("[UnityCliConnector] Failed to start HTTP server — no available port", true);
        }

        static bool TryStartOnPort(int port)
        {
            try
            {
                var listener = new HttpListener();
                listener.Prefixes.Add($"http://127.0.0.1:{port}/");
                listener.Start();

                s_Listener = listener;
                s_Port = port;
                s_Cts = new CancellationTokenSource();
                s_NextStartAttemptTime = 0;
                ClearStartFailure();

                _ = ListenLoop(s_Cts.Token);

                Debug.Log($"[UnityCliConnector] HTTP server started on port {port}");
                return true;
            }
            catch (HttpListenerException)
            {
                return false;
            }
            catch (System.Net.Sockets.SocketException)
            {
                // Windows/Mono throws SocketException instead of HttpListenerException
                return false;
            }
            catch (Exception ex)
            {
                ScheduleRetry();
                LogStartFailure($"[UnityCliConnector] Failed to start HTTP server on port {port}: {ex.Message}", true);
                return false;
            }
        }

        static void ScheduleRetry()
        {
            s_NextStartAttemptTime = EditorApplication.timeSinceStartup + AUTO_RESTART_INTERVAL;
        }

        static void LogStartFailure(string message, bool error = false)
        {
            var now = EditorApplication.timeSinceStartup;
            if (s_LastFailureMessage == message && now - s_LastFailureLogTime < FAILURE_LOG_INTERVAL)
                return;

            s_LastFailureMessage = message;
            s_LastFailureLogTime = now;
            if (error) Debug.LogError(message);
            else Debug.LogWarning(message);
        }

        static void ClearStartFailure()
        {
            s_LastFailureMessage = null;
            s_LastFailureLogTime = 0;
        }

        static void StopListener()
        {
            if (s_Listener == null) return;

            s_Cts?.Cancel();
            s_Cts?.Dispose();
            s_Cts = null;

            try
            {
                s_Listener.Stop();
                s_Listener.Close();
            }
            catch
            {
            }

            s_Listener = null;
        }

        static void Stop()
        {
            var port = s_Port;
            StopListener();
            Heartbeat.MarkStopped();
            Debug.Log($"[UnityCliConnector] HTTP server stopped (was port {port})");
        }

        static void ForceEditorUpdate()
        {
            try { EditorApplication.QueuePlayerLoopUpdate(); }
            catch { }
            try { UnityEditorInternal.InternalEditorUtility.RepaintAllViews(); }
            catch { }
        }

        static void ProcessQueue()
        {
            // Watchdog: recover if the listener died unexpectedly between assembly reloads.
            if (!IsRunning && EditorApplication.timeSinceStartup >= s_NextStartAttemptTime)
                Start();

            while (s_Queue.TryDequeue(out var item))
                ProcessItem(item);
        }

        static async void ProcessItem(WorkItem item)
        {
            try
            {
                var r = await CommandRouter.Dispatch(item.Command, item.Parameters);
                item.Tcs.SetResult(r);
            }
            catch (Exception ex)
            {
                item.Tcs.SetResult(new ErrorResponse(ex.Message));
            }
        }

        static async Task ListenLoop(CancellationToken ct)
        {
            try
            {
                while (!ct.IsCancellationRequested)
                {
                    var listener = s_Listener;
                    if (listener == null || !listener.IsListening) break;

                    try
                    {
                        var context = await listener.GetContextAsync();
                        _ = HandleRequest(context);
                    }
                    catch (ObjectDisposedException)
                    {
                        break;
                    }
                    catch (HttpListenerException)
                    {
                        break;
                    }
                }
            }
            catch (Exception ex)
            {
                Debug.LogError($"[UnityCliConnector] ListenLoop crashed: {ex.Message}");
            }
            finally
            {
                // If we exited without an explicit cancel, clean up so the watchdog can restart.
                if (!ct.IsCancellationRequested && s_Listener != null)
                {
                    Debug.LogWarning("[UnityCliConnector] ListenLoop exited unexpectedly — cleaning up for auto-recovery");
                    try { s_Listener.Stop(); s_Listener.Close(); } catch { }
                    s_Listener = null;
                    Heartbeat.MarkStopped();
                }
            }
        }

        static async Task HandleRequest(HttpListenerContext context)
        {
            var request = context.Request;
            var response = context.Response;

            response.ContentType = "application/json";

            // Block browser cross-origin requests — CLI uses Go HTTP client (not subject to CORS)
            if (request.HttpMethod == "OPTIONS")
            {
                response.StatusCode = 204;
                response.Close();
                return;
            }

            var origin = request.Headers["Origin"];
            if (origin != null)
            {
                response.StatusCode = 403;
                var buf = Encoding.UTF8.GetBytes("{\"error\":\"Browser requests are not allowed\"}");
                response.ContentLength64 = buf.Length;
                await response.OutputStream.WriteAsync(buf, 0, buf.Length);
                response.Close();
                return;
            }

            object result;

            try
            {
                if (request.HttpMethod != "POST" || request.Url.AbsolutePath != "/command")
                {
                    result = new ErrorResponse($"Expected POST /command, got {request.HttpMethod} {request.Url.AbsolutePath}");
                    response.StatusCode = 400;
                }
                else
                {
                    using var reader = new StreamReader(request.InputStream, Encoding.UTF8);
                    var body = await reader.ReadToEndAsync();
                    var json = JObject.Parse(body);

                    var command = json["command"]?.ToString();
                    var parameters = json["params"] as JObject;

                    if (string.IsNullOrEmpty(command))
                    {
                        result = new ErrorResponse("Missing 'command' field");
                        response.StatusCode = 400;
                    }
                    else
                    {
                        var tcs = new TaskCompletionSource<object>();
                        s_Queue.Enqueue(new WorkItem
                        {
                            Command = command,
                            Parameters = parameters,
                            Tcs = tcs,
                        });
                        ForceEditorUpdate();
                        result = await tcs.Task;
                    }
                }
            }
            catch (Exception ex)
            {
                result = new ErrorResponse($"Request error: {ex.Message}");
                response.StatusCode = 500;
            }

            var responseJson = JsonConvert.SerializeObject(result);
            var buffer = Encoding.UTF8.GetBytes(responseJson);
            response.ContentLength64 = buffer.Length;
            await response.OutputStream.WriteAsync(buffer, 0, buffer.Length);
            response.Close();
        }
    }
}
