using System;
using System.Diagnostics;
using System.IO;
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
        // Game 뷰 한 프레임이 안 나오면 여기서 실패한다. CLI --timeout보다 짧은 상한이다.
        private const double GameCaptureTimeoutSeconds = 30;

        // PNG 끝 IEND. ScreenCapture 쓰기가 끝나기 전에 length>0만 보면 잘린 파일이 나간다.
        private static readonly byte[] PngIend = { 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82 };

        public class Parameters
        {
            [ToolParameter("View to capture: scene (default), game", Required = false)]
            public string View { get; set; }

            [ToolParameter("Scene view width (default 1920). Ignored for game view.", Required = false)]
            public int Width { get; set; }

            [ToolParameter("Scene view height (default 1080). Ignored for game view.", Required = false)]
            public int Height { get; set; }

            [ToolParameter("Output file path, absolute or relative to project root (default: Screenshots/screenshot.png)", Required = false)]
            public string OutputPath { get; set; }
        }

        public static object HandleCommand(JObject @params)
        {
            if (@params == null)
                @params = new JObject();

            var p = new ToolParams(@params);
            var view = p.Get("view", "scene").ToLowerInvariant();
            var width = p.GetInt("width", DefaultWidth).Value;
            var height = p.GetInt("height", DefaultHeight).Value;
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
                        // Overlay UI는 camera.Render()에 안 들어간다. Game 뷰 백버퍼를 그대로 딴다.
                        return CaptureGameView(outputPath);
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

            return CaptureCamera(camera, width, height, outputPath);
        }

        // ScreenCapture는 다음 프레임에 파일을 쓴다. 메인 스레드를 막으면 프레임이 안 나오므로 Task로 기다린다.
        private static Task<object> CaptureGameView(string outputPath)
        {
            try
            {
                if (File.Exists(outputPath))
                    File.Delete(outputPath);
            }
            catch
            {
                // 이전 파일이 남아 있으면 완료 판정이 헷갈린다. 지우지 못해도 캡처는 시도한다.
            }

            ScreenCapture.CaptureScreenshot(outputPath);

            var tcs = new TaskCompletionSource<object>(TaskCreationOptions.RunContinuationsAsynchronously);
            var sw = Stopwatch.StartNew();

            void Cleanup()
            {
                EditorApplication.update -= OnUpdate;
                AssemblyReloadEvents.beforeAssemblyReload -= OnReload;
            }

            void Finish(object result)
            {
                Cleanup();
                tcs.TrySetResult(result);
            }

            void OnReload()
            {
                Finish(new ErrorResponse("Game view screenshot cancelled: domain reload"));
            }

            void OnUpdate()
            {
                if (IsPngComplete(outputPath))
                {
                    Finish(new SuccessResponse($"Screenshot saved to {outputPath}",
                        new { path = outputPath, view = "game" }));
                    return;
                }

                if (sw.Elapsed.TotalSeconds >= GameCaptureTimeoutSeconds)
                {
                    Finish(new ErrorResponse(
                        "Game view screenshot timed out waiting for ScreenCapture. Open a Game view and retry."));
                    return;
                }

                EditorApplication.QueuePlayerLoopUpdate();
            }

            EditorApplication.update += OnUpdate;
            AssemblyReloadEvents.beforeAssemblyReload += OnReload;
            EditorApplication.QueuePlayerLoopUpdate();
            return tcs.Task;
        }

        private static bool IsPngComplete(string path)
        {
            try
            {
                using var fs = new FileStream(path, FileMode.Open, FileAccess.Read, FileShare.ReadWrite);
                if (fs.Length < PngIend.Length)
                    return false;

                fs.Seek(-PngIend.Length, SeekOrigin.End);
                var tail = new byte[PngIend.Length];
                var read = 0;
                while (read < tail.Length)
                {
                    var n = fs.Read(tail, read, tail.Length - read);
                    if (n <= 0)
                        return false;
                    read += n;
                }

                for (var i = 0; i < PngIend.Length; i++)
                {
                    if (tail[i] != PngIend[i])
                        return false;
                }

                return true;
            }
            catch
            {
                return false;
            }
        }

        private static object CaptureCamera(Camera camera, int width, int height, string outputPath)
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
                    new { path = outputPath, width, height, view = "scene" });
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
