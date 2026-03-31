using System;
using System.Globalization;
using System.Linq;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    // GO 검색, Vector3 파싱, 컴포넌트 타입 해석 유틸
    public static class GameObjectResolver
    {
        // instance_id → path → name 순서로 GO 탐색 (비활성 포함)
        public static Result<GameObject> Resolve(ToolParams p)
        {
            int? instanceId = p.GetInt("instance_id");
            if (instanceId.HasValue)
                return ResolveById(instanceId.Value);

            string path = p.Get("path");
            if (!string.IsNullOrEmpty(path))
                return ResolveByPath(path);

            string name = p.Get("name");
            if (!string.IsNullOrEmpty(name))
                return ResolveByName(name);

            return Result<GameObject>.Error("Provide 'instance_id', 'path', or 'name' to identify the GameObject.");
        }

        public static Result<GameObject> ResolveById(int instanceId)
        {
            var obj = EditorUtility.InstanceIDToObject(instanceId) as GameObject;
            if (obj == null)
                return Result<GameObject>.Error($"No GameObject found with instance_id {instanceId}.");
            return Result<GameObject>.Success(obj);
        }

        public static Result<GameObject> ResolveByPath(string path)
        {
            // GameObject.Find는 활성 오브젝트만 검색
            var go = GameObject.Find(path);
            if (go != null)
                return Result<GameObject>.Success(go);

            // 비활성 포함 전수 스캔 (계층 경로 매칭)
            var all = Resources.FindObjectsOfTypeAll<GameObject>();
            foreach (var candidate in all)
            {
                // 씬 오브젝트만 (에셋 제외)
                if (!IsSceneObject(candidate)) continue;
                if (GetHierarchyPath(candidate.transform) == path)
                    return Result<GameObject>.Success(candidate);
            }

            return Result<GameObject>.Error($"No GameObject found at '{path}'.");
        }

        // 이름으로 검색 (비활성 포함)
        public static Result<GameObject> ResolveByName(string name)
        {
            // 활성 오브젝트 우선
            var go = GameObject.Find(name);
            if (go != null)
                return Result<GameObject>.Success(go);

            // 비활성 포함 전수 스캔
            var all = Resources.FindObjectsOfTypeAll<GameObject>();
            foreach (var candidate in all)
            {
                if (!IsSceneObject(candidate)) continue;
                if (candidate.name == name)
                    return Result<GameObject>.Success(candidate);
            }

            return Result<GameObject>.Error($"No GameObject found with name '{name}'.");
        }

        // 씬 오브젝트인지 판별 (프리팹 에셋/HideFlags 제외)
        static bool IsSceneObject(GameObject go)
        {
            if (go.scene.rootCount == 0 || !go.scene.isLoaded) return false;
            if (go.hideFlags == HideFlags.NotEditable || go.hideFlags == HideFlags.HideAndDontSave) return false;
            return !EditorUtility.IsPersistent(go);
        }

        // Transform → 계층 경로 문자열
        public static string GetHierarchyPath(Transform t)
        {
            if (t == null) return "";
            string path = t.name;
            while (t.parent != null)
            {
                t = t.parent;
                path = t.name + "/" + path;
            }
            return path;
        }

        // JToken → Vector3 파싱 ("1,2,3" 또는 [1,2,3])
        public static Vector3? ParseVector3(JToken token)
        {
            if (token == null) return null;

            if (token.Type == JTokenType.String)
                return ParseVector3String(token.ToString());

            if (token.Type == JTokenType.Array)
            {
                var arr = (JArray)token;
                if (arr.Count >= 3)
                {
                    return new Vector3(
                        arr[0].Value<float>(),
                        arr[1].Value<float>(),
                        arr[2].Value<float>()
                    );
                }
            }

            return null;
        }

        static Vector3? ParseVector3String(string s)
        {
            if (string.IsNullOrWhiteSpace(s)) return null;
            var parts = s.Split(',');
            if (parts.Length < 3) return null;

            if (float.TryParse(parts[0].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out float x) &&
                float.TryParse(parts[1].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out float y) &&
                float.TryParse(parts[2].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out float z))
            {
                return new Vector3(x, y, z);
            }

            return null;
        }

        // 컴포넌트 타입 이름 → Type 해석 (UnityEngine 우선)
        public static Type ResolveComponentType(string typeName)
        {
            if (string.IsNullOrWhiteSpace(typeName)) return null;

            // UnityEngine 네임스페이스 우선 시도
            var ueType = Type.GetType($"UnityEngine.{typeName}, UnityEngine.CoreModule");
            if (ueType != null && typeof(Component).IsAssignableFrom(ueType))
                return ueType;

            // 전체 어셈블리 스캔
            Type fallback = null;
            foreach (var asm in AppDomain.CurrentDomain.GetAssemblies())
            {
                try
                {
                    foreach (var type in asm.GetTypes())
                    {
                        if (type.Name == typeName && typeof(Component).IsAssignableFrom(type))
                        {
                            // UnityEngine 네임스페이스면 즉시 반환
                            if (type.Namespace != null && type.Namespace.StartsWith("UnityEngine"))
                                return type;
                            if (fallback == null)
                                fallback = type;
                        }
                    }
                }
                catch
                {
                    // ReflectionTypeLoadException 등 무시
                }
            }

            return fallback;
        }

        // 부모 GO 해석 (instance_id 또는 path/name)
        public static GameObject ResolveParent(ToolParams p, string paramName = "parent")
        {
            int? parentId = p.GetInt(paramName);
            if (parentId.HasValue)
            {
                return EditorUtility.InstanceIDToObject(parentId.Value) as GameObject;
            }

            string parentPath = p.Get(paramName);
            if (!string.IsNullOrEmpty(parentPath))
            {
                return GameObject.Find(parentPath);
            }

            return null;
        }
    }
}
