using System;
using System.IO;
using System.Reflection;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Capture a screenshot of the Unity editor. Views: scene, game, focused, full.")]
    public static class EditorScreenshot
    {
        private const int DefaultWidth = 1920;
        private const int DefaultHeight = 1080;

        public class Parameters
        {
            [ToolParameter("View to capture: scene (default), game, focused, full", Required = false)]
            public string View { get; set; }

            [ToolParameter("Override width for scene/game captures (default 1920)", Required = false)]
            public int Width { get; set; }

            [ToolParameter("Override height for scene/game captures (default 1080)", Required = false)]
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
            var outputPath = ResolveOutputPath(p.Get("outputPath"));

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
                        return CaptureGameView(width, height, outputPath);
                    case "focused":
                        return CaptureFocusedWindow(outputPath);
                    case "full":
                        return CaptureFullEditor(outputPath);
                    default:
                        return new ErrorResponse($"Unknown view '{view}'. Valid: scene, game, focused, full.");
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

            // Application.dataPath == {project}/Assets — go up one level for project root
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

        private static object CaptureGameView(int width, int height, string outputPath)
        {
            var camera = Camera.main;
            if (!camera)
            {
#if UNITY_2023_1_OR_NEWER
                camera = UnityEngine.Object.FindFirstObjectByType<Camera>();
#else
                camera = UnityEngine.Object.FindObjectOfType<Camera>();
#endif
                if (!camera)
                    return new ErrorResponse("No camera found in scene.");
            }

            return CaptureCamera(camera, width, height, outputPath);
        }

        private static object CaptureCamera(Camera camera, int width, int height, string outputPath)
        {
            var previousRT = camera.targetTexture;
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
                RenderTexture.active = null;
                if (rt) UnityEngine.Object.DestroyImmediate(rt);
                if (tex) UnityEngine.Object.DestroyImmediate(tex);
            }
        }

        // NOTE: focused and full modes read pixels directly from the screen.
        // The Unity editor must be visible (not occluded by other windows) for
        // these modes to produce correct results.

        private static object CaptureFocusedWindow(string outputPath)
        {
            var window = EditorWindow.focusedWindow;
            if (!window)
                return new ErrorResponse("No focused EditorWindow found.");

            return CaptureWindowRect(window.position, "focused window", outputPath);
        }

        private static object CaptureFullEditor(string outputPath)
        {
            var rect = GetMainEditorWindowRect();
            if (!rect.HasValue)
                return new ErrorResponse("Could not find main editor window via reflection.");

            return CaptureWindowRect(rect.Value, "full editor", outputPath);
        }

        private static object CaptureWindowRect(Rect rect, string description, string outputPath)
        {
            var w = (int)rect.width;
            var h = (int)rect.height;

            if (w <= 0 || h <= 0)
                return new ErrorResponse($"Invalid window rect: {w}x{h}");

            var colors = UnityEditorInternal.InternalEditorUtility.ReadScreenPixel(
                new Vector2(rect.x, rect.y), w, h);

            Texture2D tex = null;
            try
            {
                tex = new Texture2D(w, h, TextureFormat.RGB24, false);
                tex.SetPixels(colors);

                // ReadScreenPixel returns bottom-up; flip for correct orientation in PNG
                FlipTextureVertically(tex);
                tex.Apply();

                File.WriteAllBytes(outputPath, tex.EncodeToPNG());

                return new SuccessResponse($"Screenshot of {description} saved to {outputPath}",
                    new { path = outputPath, width = w, height = h });
            }
            finally
            {
                if (tex) UnityEngine.Object.DestroyImmediate(tex);
            }
        }

        private static void FlipTextureVertically(Texture2D tex)
        {
            var pixels = tex.GetPixels();
            var w = tex.width;
            var h = tex.height;
            var flipped = new Color[pixels.Length];

            for (var y = 0; y < h; y++)
            {
                Array.Copy(pixels, y * w, flipped, (h - 1 - y) * w, w);
            }

            tex.SetPixels(flipped);
        }

        private static Rect? GetMainEditorWindowRect()
        {
            // ContainerWindow is internal — access via reflection
            var containerWindowType = typeof(EditorWindow).Assembly.GetType("UnityEditor.ContainerWindow");
            if (containerWindowType == null)
                return null;

            var showModeField = containerWindowType.GetField("m_ShowMode",
                BindingFlags.NonPublic | BindingFlags.Instance);
            var positionProp = containerWindowType.GetProperty("position",
                BindingFlags.Public | BindingFlags.Instance);

            if (showModeField == null || positionProp == null)
                return null;

            var windows = Resources.FindObjectsOfTypeAll(containerWindowType);
            foreach (var w in windows)
            {
                // ShowMode 4 = MainWindow
                var mode = (int)showModeField.GetValue(w);
                if (mode == 4)
                    return (Rect)positionProp.GetValue(w);
            }

            return null;
        }
    }
}
