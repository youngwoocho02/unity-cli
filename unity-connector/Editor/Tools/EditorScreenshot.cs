using System;
using System.IO;
using System.Linq;
using System.Threading.Tasks;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Name = "screenshot", Description = "Capture a screenshot of the Unity editor. Views: scene, game.")]
    public static class EditorScreenshot
    {
        private const int DefaultWidth = 1920;
        private const int DefaultHeight = 1080;

        // Upper bounds on the wait for ScreenCapture to flush the PNG before we fall back
        // to the multi-camera composite. Bounded by BOTH frames and wall-clock so a stalled
        // Game view can't hold CommandRouter's global lock for an unbounded time when the
        // editor is background-throttled.
        private const int MaxCaptureFrames = 120;
        private const double MaxCaptureSeconds = 3.0;

        // Trailing bytes of every PNG: the IEND chunk type + its (constant) CRC. Their
        // presence at end-of-file is our signal that ScreenCapture finished writing.
        private static readonly byte[] PngIendSignature = { 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82 };

        public class Parameters
        {
            [ToolParameter("View to capture: scene (default), game", Required = false)]
            public string View { get; set; }

            [ToolParameter("Override width (default 1920). Game view: applies to the multi-camera fallback only.", Required = false)]
            public int Width { get; set; }

            [ToolParameter("Override height (default 1080). Game view: applies to the multi-camera fallback only.", Required = false)]
            public int Height { get; set; }

            [ToolParameter("Game view supersize multiplier 1-4 (default 1) for the primary ScreenCapture path: captures at native game-view resolution x this. Ignored by the multi-camera fallback.", Required = false)]
            public int SuperSize { get; set; }

            [ToolParameter("Output file path, absolute or relative to project root (default: Screenshots/screenshot.png)", Required = false)]
            public string OutputPath { get; set; }
        }

        public static async Task<object> HandleCommand(JObject @params)
        {
            if (@params == null)
                @params = new JObject();

            var p = new ToolParams(@params);
            var view = p.Get("view", "scene").ToLowerInvariant();
            var width = p.GetInt("width", DefaultWidth).Value;
            var height = p.GetInt("height", DefaultHeight).Value;
            var superSize = Mathf.Clamp(p.GetInt("super_size", 1).Value, 1, 4);
            var outputPath = ResolveOutputPath(p.Get("output_path"));

            try
            {
                var dir = Path.GetDirectoryName(outputPath);
                if (!string.IsNullOrEmpty(dir))
                    Directory.CreateDirectory(dir);

                switch (view)
                {
                    case "scene":
                        return CaptureSceneView(width, height, outputPath);
                    case "game":
                        return await CaptureGameView(width, height, superSize, outputPath);
                    default:
                        return new ErrorResponse($"Unknown view '{view}'. Valid: scene, game.");
                }
            }
            catch (Exception e)
            {
                return new ErrorResponse($"Screenshot failed: {e.Message}");
            }
        }

        private static string ResolveOutputPath(string userPath)
        {
            if (string.IsNullOrEmpty(userPath))
                userPath = "Screenshots/screenshot.png";

            if (Path.IsPathRooted(userPath))
                return Path.GetFullPath(userPath);

            var projectRoot = Path.GetDirectoryName(Application.dataPath);
            return Path.GetFullPath(Path.Combine(projectRoot, userPath));
        }

        private static object CaptureSceneView(int width, int height, string outputPath)
        {
            var sceneView = SceneView.lastActiveSceneView;
            if (!sceneView)
                return new ErrorResponse("No active SceneView found.");

            var camera = sceneView.camera;
            if (!camera)
                return new ErrorResponse("SceneView camera is null.");

            return CaptureSingleCamera(camera, width, height, outputPath);
        }

        // Capture exactly what the Game view shows: every stacked camera plus ALL UI,
        // including Screen Space - Overlay canvases that no single camera.Render() can
        // reproduce (overlay UI is composited onto the back buffer after camera render).
        // ScreenCapture.CaptureScreenshot grabs that composited back buffer at end of
        // frame, so we queue it and wait for the PNG to land. If no frame is produced
        // (no Game view rendering, batch mode, etc.) we fall back to compositing the
        // cameras ourselves - which the fallback's response explicitly flags as lossy.
        private static async Task<object> CaptureGameView(int width, int height, int superSize, string outputPath)
        {
            try
            {
                if (File.Exists(outputPath))
                    File.Delete(outputPath);
            }
            catch { /* a stale file just means the completion check can't assume freshness */ }

            ScreenCapture.CaptureScreenshot(outputPath, superSize);

            if (await WaitForScreenshotFile(outputPath, MaxCaptureFrames, MaxCaptureSeconds))
            {
                return new SuccessResponse($"Screenshot saved to {outputPath}",
                    new { path = outputPath, mode = "screencapture", superSize });
            }

            // No frame rendered in time - composite the cameras directly instead.
            return CaptureGameViewComposite(width, height, outputPath);
        }

        // Awaits editor frames until ScreenCapture has fully flushed the PNG (IEND written),
        // or until we hit the frame/time budget, nudging the player loop each tick so a frame
        // actually renders even in edit mode. Resolves false (-> fallback) on a domain reload
        // so an in-flight capture can never leak the update subscription or hang the request.
        private static Task<bool> WaitForScreenshotFile(string path, int maxFrames, double maxSeconds)
        {
            var tcs = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
            var sw = System.Diagnostics.Stopwatch.StartNew();
            var frames = 0;

            void Cleanup()
            {
                EditorApplication.update -= OnUpdate;
                AssemblyReloadEvents.beforeAssemblyReload -= OnBeforeReload;
            }

            void Finish(bool result)
            {
                Cleanup();
                tcs.TrySetResult(result);
            }

            void OnBeforeReload() => Finish(false);

            void OnUpdate()
            {
                frames++;

                if (IsPngComplete(path))
                {
                    Finish(true);
                    return;
                }

                if (frames >= maxFrames || sw.Elapsed.TotalSeconds >= maxSeconds)
                {
                    Finish(false);
                    return;
                }

                EditorApplication.QueuePlayerLoopUpdate();
            }

            EditorApplication.update += OnUpdate;
            AssemblyReloadEvents.beforeAssemblyReload += OnBeforeReload;
            EditorApplication.QueuePlayerLoopUpdate();
            return tcs.Task;
        }

        // True once the file exists and ends with a PNG IEND chunk, i.e. ScreenCapture has
        // finished writing it. Guards against returning a truncated image: ScreenCapture's
        // write is not atomic, so a plain length-greater-than-zero check can observe a
        // partially written file (especially at large super_size / on slow disks).
        private static bool IsPngComplete(string path)
        {
            try
            {
                using (var fs = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite))
                {
                    if (fs.Length < PngIendSignature.Length)
                        return false;

                    fs.Seek(-PngIendSignature.Length, SeekOrigin.End);

                    var tail = new byte[PngIendSignature.Length];
                    var read = 0;
                    while (read < tail.Length)
                    {
                        var n = fs.Read(tail, read, tail.Length - read);
                        if (n <= 0)
                            break;
                        read += n;
                    }
                    if (read < tail.Length)
                        return false;

                    for (var i = 0; i < PngIendSignature.Length; i++)
                        if (tail[i] != PngIendSignature[i])
                            return false;

                    return true;
                }
            }
            catch
            {
                // File missing or still locked exclusively by the writer - not ready yet.
                return false;
            }
        }

        // Fallback: composite every enabled screen-targeting camera in depth order into a
        // single render texture, honoring each camera's clear flags - this mirrors how the
        // built-in render pipeline stacks cameras onto the screen. Captures multi-camera
        // rigs and Screen Space - Camera / World Space UI, but NOT Screen Space - Overlay
        // UI (only the primary ScreenCapture path can reproduce that) - so the response
        // says so out loud. Note: camera.Render() is a built-in-pipeline API; under URP/HDRP
        // this per-camera composite is unreliable and the primary path should be relied on.
        private static object CaptureGameViewComposite(int width, int height, string outputPath)
        {
            var cameras = Camera.allCameras
                .Where(c => c && c.enabled && !c.targetTexture)
                .OrderBy(c => c.depth)
                .ToList();

            if (cameras.Count == 0)
            {
                var single = Camera.main;
#if UNITY_2023_1_OR_NEWER
                if (!single) single = UnityEngine.Object.FindFirstObjectByType<Camera>();
#else
                if (!single) single = UnityEngine.Object.FindObjectOfType<Camera>();
#endif
                if (!single)
                    return new ErrorResponse("No camera found in scene.");
                cameras.Add(single);
            }

            RenderTexture rt = null;
            Texture2D tex = null;
            var previousActive = RenderTexture.active;
            var previousTargets = new RenderTexture[cameras.Count];

            try
            {
                rt = new RenderTexture(width, height, 24);

                for (var i = 0; i < cameras.Count; i++)
                {
                    previousTargets[i] = cameras[i].targetTexture;
                    cameras[i].targetTexture = rt;
                    cameras[i].Render();
                }

                RenderTexture.active = rt;
                tex = new Texture2D(width, height, TextureFormat.RGB24, false);
                tex.ReadPixels(new Rect(0, 0, width, height), 0, 0);
                tex.Apply();

                File.WriteAllBytes(outputPath, tex.EncodeToPNG());

                return new SuccessResponse(
                    $"Screenshot saved to {outputPath} - WARNING: Game view produced no frame in time, " +
                    "so this is a multi-camera composite that OMITS Screen Space - Overlay UI. Run in play " +
                    "mode with a rendering Game view for a full capture.",
                    new { path = outputPath, width, height, mode = "composite", cameras = cameras.Count });
            }
            finally
            {
                for (var i = 0; i < cameras.Count; i++)
                    if (cameras[i]) cameras[i].targetTexture = previousTargets[i];
                RenderTexture.active = previousActive;
                if (rt) UnityEngine.Object.DestroyImmediate(rt);
                if (tex) UnityEngine.Object.DestroyImmediate(tex);
            }
        }

        // Render a single camera to an offscreen render texture. Used for the Scene view
        // (which is genuinely one editor camera) and as a primitive for the composite.
        private static object CaptureSingleCamera(Camera camera, int width, int height, string outputPath)
        {
            var previousRT = camera.targetTexture;
            var previousActive = RenderTexture.active;
            RenderTexture rt = null;
            Texture2D tex = null;

            try
            {
                rt = new RenderTexture(width, height, 24);
                camera.targetTexture = rt;
                camera.Render();

                RenderTexture.active = rt;
                tex = new Texture2D(width, height, TextureFormat.RGB24, false);
                tex.ReadPixels(new Rect(0, 0, width, height), 0, 0);
                tex.Apply();

                File.WriteAllBytes(outputPath, tex.EncodeToPNG());

                return new SuccessResponse($"Screenshot saved to {outputPath}",
                    new { path = outputPath, width, height });
            }
            finally
            {
                camera.targetTexture = previousRT;
                RenderTexture.active = previousActive;
                if (rt) UnityEngine.Object.DestroyImmediate(rt);
                if (tex) UnityEngine.Object.DestroyImmediate(tex);
            }
        }
    }
}
