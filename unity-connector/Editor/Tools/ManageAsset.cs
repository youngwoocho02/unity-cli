using System.Collections.Generic;
using System.IO;
using System.Linq;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Name = "asset", Description = "Manage assets. Actions: search, get_info, import, delete, create_folder, move, duplicate.", Group = "assets")]
    public static class ManageAsset
    {
        public class Parameters
        {
            [ToolParameter("Action: search, get_info, import, delete, create_folder, move, duplicate", Required = true)]
            public string Action { get; set; }

            [ToolParameter("Asset path (e.g. Assets/Materials/MyMat.mat)")]
            public string Path { get; set; }

            [ToolParameter("Search filter (Unity syntax: t:Material, t:Prefab, name)")]
            public string Filter { get; set; }

            [ToolParameter("Search folder scope (e.g. Assets/Prefabs)")]
            public string Folder { get; set; }

            [ToolParameter("Destination path for move/duplicate")]
            public string Destination { get; set; }

            [ToolParameter("Page size for search results (default 50)")]
            public int PageSize { get; set; }

            [ToolParameter("Cursor offset for pagination")]
            public int Cursor { get; set; }
        }

        public static object HandleCommand(JObject @params)
        {
            if (@params == null)
                return new ErrorResponse("Parameters cannot be null.");

            var p = new ToolParams(@params);
            var actionResult = p.GetRequired("action");
            if (!actionResult.IsSuccess)
                return new ErrorResponse(actionResult.ErrorMessage);

            // 읽기 전용 액션
            switch (actionResult.Value.ToLowerInvariant())
            {
                case "search": return Search(p);
                case "get_info": return GetInfo(p);
            }

            // 변경 액션
            // Note: create_folder, delete 등은 AssetDatabase 변경으로 도메인 리로드가
            // 발생할 수 있어 HTTP 응답이 유실될 수 있음. 동작 자체는 정상 수행됨.
            switch (actionResult.Value.ToLowerInvariant())
            {
                case "import": return Import(p);
                case "delete": return Delete(p);
                case "create_folder": return CreateFolder(p);
                case "move": return Move(p);
                case "duplicate": return DuplicateAsset(p);
                default:
                    return new ErrorResponse($"Unknown action: '{actionResult.Value}'.");
            }
        }

        static object Search(ToolParams p)
        {
            string filter = p.Get("filter", "");
            string folder = p.Get("folder");
            string[] searchFolders = string.IsNullOrEmpty(folder) ? null : new[] { NormalizePath(folder) };

            string[] guids = searchFolders != null
                ? AssetDatabase.FindAssets(filter, searchFolders)
                : AssetDatabase.FindAssets(filter);

            int pageSize = p.GetInt("page_size") ?? 50;
            int cursor = p.GetInt("cursor") ?? 0;
            int total = guids.Length;

            var items = guids
                .Skip(cursor)
                .Take(pageSize)
                .Select(guid =>
                {
                    string path = AssetDatabase.GUIDToAssetPath(guid);
                    var type = AssetDatabase.GetMainAssetTypeAtPath(path);
                    return new
                    {
                        guid,
                        path,
                        type = type?.Name ?? "Unknown"
                    };
                })
                .ToArray();

            int? nextCursor = cursor + pageSize < total ? cursor + pageSize : (int?)null;

            return new SuccessResponse($"Found {total} assets.", new
            {
                total,
                page_size = pageSize,
                cursor,
                next_cursor = nextCursor,
                items
            });
        }

        static object GetInfo(ToolParams p)
        {
            var pathResult = p.GetRequired("path", "'path' is required.");
            if (!pathResult.IsSuccess) return new ErrorResponse(pathResult.ErrorMessage);

            string path = NormalizePath(pathResult.Value);
            string guid = AssetDatabase.AssetPathToGUID(path);
            if (string.IsNullOrEmpty(guid))
                return new ErrorResponse($"Asset not found at '{path}'.");

            var type = AssetDatabase.GetMainAssetTypeAtPath(path);
            var obj = AssetDatabase.LoadAssetAtPath<Object>(path);

            long sizeBytes = 0;
            string fullPath = System.IO.Path.GetFullPath(path);
            if (File.Exists(fullPath))
                sizeBytes = new FileInfo(fullPath).Length;

            string[] labels = obj != null ? AssetDatabase.GetLabels(obj) : new string[0];
            string[] dependencies = AssetDatabase.GetDependencies(path, false)
                .Where(d => d != path)
                .ToArray();

            return new SuccessResponse($"Asset info: {path}.", new
            {
                path,
                guid,
                type = type?.Name ?? "Unknown",
                size_bytes = sizeBytes,
                labels,
                dependencies
            });
        }

        static object Import(ToolParams p)
        {
            var pathResult = p.GetRequired("path", "'path' is required.");
            if (!pathResult.IsSuccess) return new ErrorResponse(pathResult.ErrorMessage);

            string path = NormalizePath(pathResult.Value);
            AssetDatabase.ImportAsset(path, ImportAssetOptions.ForceUpdate);

            return new SuccessResponse($"Imported '{path}'.", new { imported = path });
        }

        static object Delete(ToolParams p)
        {
            var pathResult = p.GetRequired("path", "'path' is required.");
            if (!pathResult.IsSuccess) return new ErrorResponse(pathResult.ErrorMessage);

            string path = NormalizePath(pathResult.Value);
            if (!path.StartsWith("Assets/"))
                return new ErrorResponse("Path must start with 'Assets/'.");

            string guid = AssetDatabase.AssetPathToGUID(path);
            if (string.IsNullOrEmpty(guid))
                return new ErrorResponse($"Asset not found at '{path}'.");

            bool deleted = AssetDatabase.DeleteAsset(path);
            if (!deleted)
                return new ErrorResponse($"Failed to delete '{path}'.");

            return new SuccessResponse($"Deleted '{path}'.", new { deleted = path });
        }

        static object CreateFolder(ToolParams p)
        {
            var pathResult = p.GetRequired("path", "'path' is required.");
            if (!pathResult.IsSuccess) return new ErrorResponse(pathResult.ErrorMessage);

            string path = NormalizePath(pathResult.Value);
            if (!path.StartsWith("Assets/") && path != "Assets")
                return new ErrorResponse("Path must start with 'Assets/'.");

            // 이미 존재하는지 확인
            if (AssetDatabase.IsValidFolder(path))
                return new SuccessResponse($"Folder already exists: '{path}'.", new
                {
                    created = path,
                    guid = AssetDatabase.AssetPathToGUID(path)
                });

            // 부모/자식 분리
            int lastSlash = path.LastIndexOf('/');
            if (lastSlash < 0)
                return new ErrorResponse("Invalid folder path.");

            string parent = path.Substring(0, lastSlash);
            string folderName = path.Substring(lastSlash + 1);

            // 부모 폴더 재귀 생성
            if (!AssetDatabase.IsValidFolder(parent))
            {
                var parentResult = CreateFolderRecursive(parent);
                if (parentResult != null) return parentResult;
            }

            string guid = AssetDatabase.CreateFolder(parent, folderName);

            return new SuccessResponse($"Created folder '{path}'.", new
            {
                created = path,
                guid
            });
        }

        static ErrorResponse CreateFolderRecursive(string path)
        {
            if (AssetDatabase.IsValidFolder(path)) return null;
            if (!path.StartsWith("Assets")) return new ErrorResponse($"Invalid path: '{path}'.");

            int lastSlash = path.LastIndexOf('/');
            if (lastSlash < 0) return new ErrorResponse($"Cannot create root folder: '{path}'.");

            string parent = path.Substring(0, lastSlash);
            string name = path.Substring(lastSlash + 1);

            var parentError = CreateFolderRecursive(parent);
            if (parentError != null) return parentError;

            AssetDatabase.CreateFolder(parent, name);
            return null;
        }

        static object Move(ToolParams p)
        {
            var pathResult = p.GetRequired("path", "'path' is required.");
            if (!pathResult.IsSuccess) return new ErrorResponse(pathResult.ErrorMessage);

            var destResult = p.GetRequired("destination", "'destination' is required.");
            if (!destResult.IsSuccess) return new ErrorResponse(destResult.ErrorMessage);

            string from = NormalizePath(pathResult.Value);
            string to = NormalizePath(destResult.Value);

            string error = AssetDatabase.MoveAsset(from, to);
            if (!string.IsNullOrEmpty(error))
                return new ErrorResponse($"Move failed: {error}");

            return new SuccessResponse($"Moved '{from}' → '{to}'.", new
            {
                moved_from = from,
                moved_to = to
            });
        }

        static object DuplicateAsset(ToolParams p)
        {
            var pathResult = p.GetRequired("path", "'path' is required.");
            if (!pathResult.IsSuccess) return new ErrorResponse(pathResult.ErrorMessage);

            string from = NormalizePath(pathResult.Value);
            string to = p.Get("destination");

            if (string.IsNullOrEmpty(to))
                to = AssetDatabase.GenerateUniqueAssetPath(from);
            else
                to = NormalizePath(to);

            bool success = AssetDatabase.CopyAsset(from, to);
            if (!success)
                return new ErrorResponse($"Failed to duplicate '{from}'.");

            return new SuccessResponse($"Duplicated '{from}'.", new
            {
                original = from,
                duplicate = to
            });
        }

        // Assets/ 프리픽스 정규화
        static string NormalizePath(string path)
        {
            if (string.IsNullOrEmpty(path)) return path;
            path = path.Replace('\\', '/');
            return path;
        }
    }
}
