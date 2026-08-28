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
    /// Returns 503 while Unity is compiling, reloading, or otherwise unable to run commands.
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
        static SynchronizationContext s_MainContext;
        static double s_NextStartAttemptTime;
        static string s_LastFailureMessage;
        static double s_LastFailureLogTime;
        static bool s_Stopping;
        static bool s_RestartPending;

        static readonly ConcurrentQueue<WorkItem> s_Queue = new();

        struct WorkItem
        {
            public string Command;
            public JObject Parameters;
            public TaskCompletionSource<object> Tcs;
        }

        static HttpServer()
        {
            s_MainContext = SynchronizationContext.Current;
            Start();
            // 리로드 전에 리스너를 닫고, 리로드 후 다시 연다. 큐는 메인 스레드 update에서 뺀다.
            EditorApplication.quitting += Stop;
            AssemblyReloadEvents.beforeAssemblyReload += StopListener;
            AssemblyReloadEvents.afterAssemblyReload += Start;
            EditorApplication.update += ProcessQueue;
        }

        public static int Port => s_Port;
        public static bool IsRunning => s_Listener != null && s_Listener.IsListening;

        // 8090부터 10개 포트를 시도한다. 전부 실패하면 heartbeat를 stopped로 두고 1초 뒤 재시도.
        static void Start()
        {
            s_Stopping = false;
            if (IsRunning) return;
            if (s_Listener != null) StopListener();
            s_Stopping = false;

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
                var cts = new CancellationTokenSource();
                s_Cts = cts;
                ClearRetry();
                ClearStartFailure();

                _ = ListenLoop(listener, cts);

                Debug.Log("[UnityCliConnector] HTTP server started");
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
                LogStartFailure($"[UnityCliConnector] Failed to start HTTP server: {ex.Message}", true);
                return false;
            }
        }

        static void ScheduleRetry()
        {
            s_NextStartAttemptTime = EditorApplication.timeSinceStartup + AUTO_RESTART_INTERVAL;
        }

        static void ClearRetry()
        {
            s_NextStartAttemptTime = 0;
        }

        // 같은 실패 로그를 5초에 한 번만 찍어 콘솔을 채우지 않는다.
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
            s_Stopping = true;
            s_RestartPending = false;
            ClearRetry();

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
            StopListener();
            Heartbeat.MarkStopped();
            Debug.Log("[UnityCliConnector] HTTP server stopped");
        }

        // 포커스가 없어도 플레이어 루프/뷰를 깨워 큐가 빨리 빠지게 한다.
        static void ForceEditorUpdate()
        {
            s_MainContext?.Post(_ =>
            {
                try { EditorApplication.QueuePlayerLoopUpdate(); }
                catch { }
                try { UnityEditorInternal.InternalEditorUtility.RepaintAllViews(); }
                catch { }
            }, null);
        }

        static void ProcessQueue()
        {
            // ListenLoop가 죽으면 여기서 재시작한다. 핸들러 실행과 분리한다.
            if (s_RestartPending)
            {
                s_RestartPending = false;
                Heartbeat.MarkStopped();
                ScheduleRetry();
            }

            if (!IsRunning && s_NextStartAttemptTime > 0 && EditorApplication.timeSinceStartup >= s_NextStartAttemptTime)
                Start();

            while (s_Queue.TryDequeue(out var item))
                ProcessItem(item);
        }

        static async void ProcessItem(WorkItem item)
        {
            try
            {
                var r = await CommandRouter.Dispatch(item.Command, item.Parameters);
                item.Tcs.TrySetResult(r);
            }
            catch (Exception ex)
            {
                item.Tcs.TrySetResult(new ErrorResponse(ex.Message));
            }
        }

        static async Task ListenLoop(HttpListener listener, CancellationTokenSource cts)
        {
            var ct = cts.Token;
            try
            {
                while (!ct.IsCancellationRequested)
                {
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
                System.Diagnostics.Debug.WriteLine($"[UnityCliConnector] ListenLoop crashed: {ex.Message}");
            }
            finally
            {
                // 정상 Stop이 아니면 리스너가 죽은 것이다. 메인 스레드 틱에서 다시 연다.
                if (!ct.IsCancellationRequested && !s_Stopping && ReferenceEquals(s_Listener, listener))
                {
                    try
                    {
                        listener.Stop();
                        listener.Close();
                    }
                    catch
                    {
                    }
                    s_Listener = null;
                    if (ReferenceEquals(s_Cts, cts))
                        s_Cts = null;
                    s_RestartPending = true;
                }
            }
        }

        // GET /health는 즉시 스냅샷. POST /command는 받을 수 있을 때만 큐에 넣고 결과를 기다린다.
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
                if (request.HttpMethod == "GET" && request.Url.AbsolutePath == "/health")
                {
                    result = new SuccessResponse("ok", Heartbeat.HealthSnapshot());
                }
                else if (request.HttpMethod != "POST" || request.Url.AbsolutePath != "/command")
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
                    else if (!Heartbeat.CanRunCommands())
                    {
                        // CLI는 503을 일시 오류로 보고 Health가 ready가 될 때까지 다시 보낸다.
                        result = new ErrorResponse($"unity is not accepting commands ({Heartbeat.CurrentState})");
                        response.StatusCode = 503;
                    }
                    else
                    {
                        var tcs = new TaskCompletionSource<object>(TaskCreationOptions.RunContinuationsAsynchronously);
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
