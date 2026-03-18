using System.IO;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Force reserialize Unity assets. No params = entire project.")]
    public static class ReserializeAssets
    {
        public class Parameters
        {
            [ToolParameter("Single asset path to reserialize")]
            public string Path { get; set; }

            [ToolParameter("Multiple asset paths to reserialize")]
            public string[] Paths { get; set; }
        }

        static bool IsValidAssetPath(string path)
        {
            if (string.IsNullOrEmpty(path)) return false;
            // Normalize separators and resolve . / ..
            var normalized = System.IO.Path.GetFullPath(path).Replace('\\', '/');
            var assetsRoot = System.IO.Path.GetFullPath("Assets").Replace('\\', '/');
            // After canonicalization, path must be under Assets/
            if (!normalized.StartsWith(assetsRoot)) return false;
            // Reject raw paths containing .. (defense-in-depth)
            if (path.Contains("..")) return false;
            return true;
        }

        public static object HandleCommand(JObject parameters)
        {
            var pathToken = parameters["path"];
            var pathsToken = parameters["paths"];

            string[] paths;
            if (pathsToken != null && pathsToken.Type == JTokenType.Array)
                paths = pathsToken.ToObject<string[]>();
            else if (pathToken != null)
                paths = new[] { pathToken.ToString() };
            else
                paths = null;

            if (paths == null || paths.Length == 0)
            {
                AssetDatabase.ForceReserializeAssets();
                Debug.Log("[UnityCliConnector] ForceReserializeAssets: entire project");
                return new SuccessResponse("Reserialized entire project");
            }

            // Validate all paths before processing
            foreach (var p in paths)
            {
                if (!IsValidAssetPath(p))
                    return new ErrorResponse($"Invalid asset path: '{p}'. Paths must be under Assets/ and must not contain '..'");
            }

            AssetDatabase.ForceReserializeAssets(paths);
            Debug.Log($"[UnityCliConnector] ForceReserializeAssets: {string.Join(", ", paths)}");
            return new SuccessResponse($"Reserialized {paths.Length} asset(s)", new { paths });
        }
    }
}
