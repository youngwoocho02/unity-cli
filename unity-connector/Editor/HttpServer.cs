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
    /// Uses ConcurrentQueue + EditorApplication.update for main-thread marshaling
    /// so commands execute even when Unity is unfocused.
    /// Survives domain reloads via InitializeOnLoad.
    /// </summary>
    [InitializeOnLoad]
    public static class HttpServer
    {
        const int DEFAULT_PORT = 8090;
        const int MAX_PORT_ATTEMPTS = 10;
        const int MaxRequestBodyBytes = 1_048_576; // 1 MB

        static HttpListener s_Listener;
        static CancellationTokenSource s_Cts;
        static int s_Port;
        static string s_AuthToken;

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
            EditorApplication.projectChanged += Restart;
            EditorApplication.update += ProcessQueue;
        }

        static void Restart()
        {
            StopListener();
            Start();
        }

        public static int Port => s_Port;
        public static string AuthToken => s_AuthToken;

        static void Start()
        {
            if (s_Listener != null) return;

            for (var attempt = 0; attempt < MAX_PORT_ATTEMPTS; attempt++)
            {
                var port = DEFAULT_PORT + attempt;
                try
                {
                    var listener = new HttpListener();
                    listener.Prefixes.Add($"http://127.0.0.1:{port}/");
                    listener.Start();

                    s_Listener = listener;
                    s_Port = port;
                    s_AuthToken = Guid.NewGuid().ToString("N");
                    s_Cts = new CancellationTokenSource();

                    _ = ListenLoop(s_Cts.Token);

                    InstanceRegistry.Register(port, s_AuthToken);
                    Debug.Log($"[UnityCliConnector] HTTP server started on port {port}");
                    return;
                }
                catch (HttpListenerException)
                {
                    // Port in use, try next
                }
                catch (System.Net.Sockets.SocketException)
                {
                    // Windows/Mono throws SocketException instead of HttpListenerException
                }
            }

            Debug.LogError("[UnityCliConnector] Failed to start HTTP server — no available port");
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
            InstanceRegistry.Unregister();
            Debug.Log($"[UnityCliConnector] HTTP server stopped (was port {port})");
        }

        static void ForceEditorUpdate()
        {
            try { UnityEditorInternal.InternalEditorUtility.RepaintAllViews(); }
            catch { }
        }

        static void ProcessQueue()
        {
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
            while (ct.IsCancellationRequested == false && s_Listener?.IsListening == true)
            {
                try
                {
                    var context = await s_Listener.GetContextAsync();
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

        static bool IsLocalhostOrigin(string origin)
        {
            if (string.IsNullOrEmpty(origin)) return false;
            if (Uri.TryCreate(origin, UriKind.Absolute, out var uri))
            {
                var host = uri.Host;
                return host == "localhost" || host == "127.0.0.1" || host == "[::1]";
            }
            return false;
        }

        static void SetCorsHeaders(HttpListenerResponse response, string origin)
        {
            if (IsLocalhostOrigin(origin))
            {
                response.Headers.Add("Access-Control-Allow-Origin", origin);
                response.Headers.Add("Access-Control-Allow-Methods", "POST, OPTIONS");
                response.Headers.Add("Access-Control-Allow-Headers", "Content-Type, Authorization");
                response.Headers.Add("Vary", "Origin");
            }
        }

        static bool ValidateAuth(HttpListenerRequest request)
        {
            if (string.IsNullOrEmpty(s_AuthToken)) return false;
            var header = request.Headers["Authorization"];
            if (string.IsNullOrEmpty(header)) return false;
            if (!header.StartsWith("Bearer ", StringComparison.OrdinalIgnoreCase)) return false;
            var token = header.Substring(7).Trim();
            return string.Equals(token, s_AuthToken, StringComparison.Ordinal);
        }

        static async Task WriteResponse(HttpListenerResponse response, object result)
        {
            var responseJson = JsonConvert.SerializeObject(result);
            var buffer = Encoding.UTF8.GetBytes(responseJson);
            response.ContentLength64 = buffer.Length;
            await response.OutputStream.WriteAsync(buffer, 0, buffer.Length);
            response.Close();
        }

        static async Task HandleRequest(HttpListenerContext context)
        {
            var request = context.Request;
            var response = context.Response;

            response.ContentType = "application/json";
            var origin = request.Headers["Origin"];
            SetCorsHeaders(response, origin);

            if (request.HttpMethod == "OPTIONS")
            {
                response.StatusCode = 204;
                response.Close();
                return;
            }

            try
            {
                if (request.HttpMethod != "POST" || request.Url.AbsolutePath != "/command")
                {
                    response.StatusCode = 400;
                    await WriteResponse(response, new ErrorResponse($"Expected POST /command, got {request.HttpMethod} {request.Url.AbsolutePath}"));
                    return;
                }

                if (!ValidateAuth(request))
                {
                    response.StatusCode = 401;
                    await WriteResponse(response, new ErrorResponse("Unauthorized"));
                    return;
                }

                if (request.ContentLength64 > MaxRequestBodyBytes)
                {
                    response.StatusCode = 413;
                    await WriteResponse(response, new ErrorResponse($"Request body too large (max {MaxRequestBodyBytes} bytes)"));
                    return;
                }

                using var reader = new StreamReader(request.InputStream, Encoding.UTF8);
                var body = await reader.ReadToEndAsync();

                if (body.Length > MaxRequestBodyBytes)
                {
                    response.StatusCode = 413;
                    await WriteResponse(response, new ErrorResponse($"Request body too large (max {MaxRequestBodyBytes} bytes)"));
                    return;
                }

                var json = JObject.Parse(body);
                var command = json["command"]?.ToString();
                var parameters = json["params"] as JObject;

                if (string.IsNullOrEmpty(command))
                {
                    response.StatusCode = 400;
                    await WriteResponse(response, new ErrorResponse("Missing 'command' field"));
                    return;
                }

                var tcs = new TaskCompletionSource<object>();
                s_Queue.Enqueue(new WorkItem
                {
                    Command = command,
                    Parameters = parameters,
                    Tcs = tcs,
                });
                ForceEditorUpdate();
                await WriteResponse(response, await tcs.Task);
            }
            catch (Exception ex)
            {
                response.StatusCode = 500;
                await WriteResponse(response, new ErrorResponse($"Request error: {ex.Message}"));
            }
        }
    }
}
