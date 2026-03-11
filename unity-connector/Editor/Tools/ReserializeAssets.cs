using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Force reserialize Unity assets by path.")]
    public static class ReserializeAssets
    {
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
                return new ErrorResponse("'path' or 'paths' parameter required");

            if (paths.Length == 0)
                return new ErrorResponse("No paths provided");

            AssetDatabase.ForceReserializeAssets(paths);
            Debug.Log($"[UnityCliConnector] ForceReserializeAssets: {string.Join(", ", paths)}");
            return new SuccessResponse($"Reserialized {paths.Length} asset(s)", new { paths });
        }
    }
}
