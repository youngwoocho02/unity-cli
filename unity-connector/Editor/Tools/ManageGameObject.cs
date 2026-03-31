using System;
using System.Collections.Generic;
using System.Linq;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;
using UnityEngine.SceneManagement;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Name = "gameobject", Description = "Manage GameObjects. Actions: find, create, create_primitive, destroy, set_transform, set_active, get_hierarchy, get_components, add_component, remove_component, rename, duplicate, set_parent.", Group = "scene")]
    public static class ManageGameObject
    {
        public class Parameters
        {
            [ToolParameter("Action: find, create, create_primitive, destroy, set_transform, set_active, get_hierarchy, get_components, add_component, remove_component, rename, duplicate, set_parent", Required = true)]
            public string Action { get; set; }

            [ToolParameter("Instance ID of target GameObject")]
            public int InstanceId { get; set; }

            [ToolParameter("Hierarchy path or name of target GameObject")]
            public string Path { get; set; }

            [ToolParameter("Name for find/create/rename")]
            public string Name { get; set; }

            [ToolParameter("Tag filter for find")]
            public string Tag { get; set; }

            [ToolParameter("Layer filter for find")]
            public string Layer { get; set; }

            [ToolParameter("Component type name for find/add/remove")]
            public string ComponentType { get; set; }

            [ToolParameter("Primitive type: Cube, Sphere, Capsule, Cylinder, Plane, Quad")]
            public string PrimitiveType { get; set; }

            [ToolParameter("Position as x,y,z or [x,y,z]")]
            public string Position { get; set; }

            [ToolParameter("Rotation euler angles as x,y,z or [x,y,z]")]
            public string Rotation { get; set; }

            [ToolParameter("Scale as x,y,z or [x,y,z]")]
            public string Scale { get; set; }

            [ToolParameter("Parent instance ID or path")]
            public string Parent { get; set; }

            [ToolParameter("Active state for set_active")]
            public bool Active { get; set; }

            [ToolParameter("New name for rename")]
            public string NewName { get; set; }

            [ToolParameter("Max hierarchy depth (default 3)")]
            public int MaxDepth { get; set; }

            [ToolParameter("Page size for results (default 50)")]
            public int PageSize { get; set; }

            [ToolParameter("Cursor offset for pagination")]
            public int Cursor { get; set; }

            [ToolParameter("Include inactive GameObjects in find")]
            public bool IncludeInactive { get; set; }
        }

        public static object HandleCommand(JObject @params)
        {
            if (@params == null)
                return new ErrorResponse("Parameters cannot be null.");

            var p = new ToolParams(@params);
            var actionResult = p.GetRequired("action");
            if (!actionResult.IsSuccess)
                return new ErrorResponse(actionResult.ErrorMessage);

            switch (actionResult.Value.ToLowerInvariant())
            {
                case "find": return Find(p);
                case "create": return Create(p);
                case "create_primitive": return CreatePrimitive(p);
                case "destroy": return Destroy(p);
                case "set_transform": return SetTransform(p);
                case "set_active": return SetActive(p);
                case "get_hierarchy": return GetHierarchy(p);
                case "get_components": return GetComponents(p);
                case "add_component": return AddComponent(p);
                case "remove_component": return RemoveComponent(p);
                case "rename": return Rename(p);
                case "duplicate": return Duplicate(p);
                case "set_parent": return SetParent(p);
                default:
                    return new ErrorResponse($"Unknown action: '{actionResult.Value}'.");
            }
        }

        static object Find(ToolParams p)
        {
            var allObjects = GameObject.FindObjectsByType<GameObject>(FindObjectsSortMode.InstanceID);
            var results = new List<object>();

            string nameFilter = p.Get("name");
            string tagFilter = p.Get("tag");
            string layerFilter = p.Get("layer");
            string componentFilter = p.Get("component_type");

            foreach (var go in allObjects)
            {
                if (!string.IsNullOrEmpty(nameFilter) &&
                    go.name.IndexOf(nameFilter, StringComparison.OrdinalIgnoreCase) < 0)
                    continue;

                if (!string.IsNullOrEmpty(tagFilter) && !go.CompareTag(tagFilter))
                    continue;

                if (!string.IsNullOrEmpty(layerFilter) &&
                    !LayerMask.LayerToName(go.layer).Equals(layerFilter, StringComparison.OrdinalIgnoreCase))
                    continue;

                if (!string.IsNullOrEmpty(componentFilter))
                {
                    var type = GameObjectResolver.ResolveComponentType(componentFilter);
                    if (type == null || go.GetComponent(type) == null)
                        continue;
                }

                results.Add(new
                {
                    instance_id = go.GetInstanceID(),
                    name = go.name,
                    path = GameObjectResolver.GetHierarchyPath(go.transform),
                    tag = go.tag,
                    layer = LayerMask.LayerToName(go.layer),
                    active = go.activeInHierarchy
                });
            }

            int pageSize = p.GetInt("page_size") ?? 50;
            int cursor = p.GetInt("cursor") ?? 0;
            int total = results.Count;
            var page = results.Skip(cursor).Take(pageSize).ToList();
            int? nextCursor = cursor + pageSize < total ? cursor + pageSize : (int?)null;

            return new SuccessResponse($"Found {total} GameObjects.", new
            {
                total,
                page_size = pageSize,
                cursor,
                next_cursor = nextCursor,
                items = page
            });
        }

        static object Create(ToolParams p)
        {
            string name = p.Get("name", "GameObject");
            var go = new GameObject(name);
            Undo.RegisterCreatedObjectUndo(go, $"Create {name}");

            ApplyTransform(go, p);

            var parent = GameObjectResolver.ResolveParent(p);
            if (parent != null)
            {
                Undo.SetTransformParent(go.transform, parent.transform, $"Set parent of {name}");
            }

            return new SuccessResponse($"Created '{name}'.", new
            {
                instance_id = go.GetInstanceID(),
                name = go.name,
                path = GameObjectResolver.GetHierarchyPath(go.transform)
            });
        }

        static object CreatePrimitive(ToolParams p)
        {
            var typeStr = p.Get("primitive_type");
            if (string.IsNullOrEmpty(typeStr))
                return new ErrorResponse("'primitive_type' is required. Options: Cube, Sphere, Capsule, Cylinder, Plane, Quad.");

            if (!Enum.TryParse<PrimitiveType>(typeStr, true, out var primitiveType))
                return new ErrorResponse($"Invalid primitive_type '{typeStr}'. Options: Cube, Sphere, Capsule, Cylinder, Plane, Quad.");

            var go = GameObject.CreatePrimitive(primitiveType);
            string name = p.Get("name");
            if (!string.IsNullOrEmpty(name))
                go.name = name;

            Undo.RegisterCreatedObjectUndo(go, $"Create {go.name}");
            ApplyTransform(go, p);

            return new SuccessResponse($"Created primitive '{go.name}'.", new
            {
                instance_id = go.GetInstanceID(),
                name = go.name,
                path = GameObjectResolver.GetHierarchyPath(go.transform)
            });
        }

        static object Destroy(ToolParams p)
        {
            var result = GameObjectResolver.Resolve(p);
            if (!result.IsSuccess) return new ErrorResponse(result.ErrorMessage);

            string name = result.Value.name;
            Undo.DestroyObjectImmediate(result.Value);

            return new SuccessResponse($"Destroyed '{name}'.", new { destroyed = name });
        }

        static object SetTransform(ToolParams p)
        {
            var result = GameObjectResolver.Resolve(p);
            if (!result.IsSuccess) return new ErrorResponse(result.ErrorMessage);

            var go = result.Value;
            Undo.RecordObject(go.transform, "Set Transform");
            ApplyTransform(go, p);

            var t = go.transform;
            return new SuccessResponse($"Transform updated for '{go.name}'.", new
            {
                instance_id = go.GetInstanceID(),
                position = new[] { t.localPosition.x, t.localPosition.y, t.localPosition.z },
                rotation = new[] { t.localEulerAngles.x, t.localEulerAngles.y, t.localEulerAngles.z },
                scale = new[] { t.localScale.x, t.localScale.y, t.localScale.z }
            });
        }

        static object SetActive(ToolParams p)
        {
            var result = GameObjectResolver.Resolve(p);
            if (!result.IsSuccess) return new ErrorResponse(result.ErrorMessage);

            var go = result.Value;
            bool active = p.GetBool("active", true);
            Undo.RecordObject(go, "Set Active");
            go.SetActive(active);

            return new SuccessResponse($"'{go.name}' active = {active}.", new
            {
                instance_id = go.GetInstanceID(),
                active
            });
        }

        static object GetHierarchy(ToolParams p)
        {
            int maxDepth = p.GetInt("max_depth") ?? 3;
            var scene = SceneManager.GetActiveScene();

            // 특정 GO 지정 시 그 하위만
            var targetResult = GameObjectResolver.Resolve(p);
            if (targetResult.IsSuccess)
            {
                var node = BuildNode(targetResult.Value.transform, 0, maxDepth);
                return new SuccessResponse("Hierarchy retrieved.", new
                {
                    scene = scene.name,
                    roots = new[] { node }
                });
            }

            // 전체 루트
            var roots = scene.GetRootGameObjects()
                .Select(go => BuildNode(go.transform, 0, maxDepth))
                .ToArray();

            return new SuccessResponse("Hierarchy retrieved.", new
            {
                scene = scene.name,
                roots
            });
        }

        static object BuildNode(Transform t, int depth, int maxDepth)
        {
            var children = new List<object>();
            if (depth < maxDepth)
            {
                for (int i = 0; i < t.childCount; i++)
                    children.Add(BuildNode(t.GetChild(i), depth + 1, maxDepth));
            }

            return new
            {
                instance_id = t.gameObject.GetInstanceID(),
                name = t.name,
                active = t.gameObject.activeSelf,
                children = children.Count > 0 ? children : null
            };
        }

        static object GetComponents(ToolParams p)
        {
            var result = GameObjectResolver.Resolve(p);
            if (!result.IsSuccess) return new ErrorResponse(result.ErrorMessage);

            var go = result.Value;
            var components = go.GetComponents<Component>()
                .Where(c => c != null) // null = missing script
                .Select(c => new
                {
                    type = c.GetType().Name,
                    instance_id = c.GetInstanceID(),
                    enabled = c is Behaviour b ? b.enabled : true
                })
                .ToArray();

            return new SuccessResponse($"{components.Length} components on '{go.name}'.", new
            {
                instance_id = go.GetInstanceID(),
                components
            });
        }

        static object AddComponent(ToolParams p)
        {
            var result = GameObjectResolver.Resolve(p);
            if (!result.IsSuccess) return new ErrorResponse(result.ErrorMessage);

            var typeName = p.Get("component_type");
            if (string.IsNullOrEmpty(typeName))
                return new ErrorResponse("'component_type' is required.");

            var type = GameObjectResolver.ResolveComponentType(typeName);
            if (type == null)
                return new ErrorResponse($"Component type '{typeName}' not found.");

            var go = result.Value;
            var component = ObjectFactory.AddComponent(go, type);

            return new SuccessResponse($"Added {type.Name} to '{go.name}'.", new
            {
                instance_id = go.GetInstanceID(),
                component_type = type.Name,
                component_instance_id = component.GetInstanceID()
            });
        }

        static object RemoveComponent(ToolParams p)
        {
            var result = GameObjectResolver.Resolve(p);
            if (!result.IsSuccess) return new ErrorResponse(result.ErrorMessage);

            var typeName = p.Get("component_type");
            if (string.IsNullOrEmpty(typeName))
                return new ErrorResponse("'component_type' is required.");

            var type = GameObjectResolver.ResolveComponentType(typeName);
            if (type == null)
                return new ErrorResponse($"Component type '{typeName}' not found.");

            var go = result.Value;
            var component = go.GetComponent(type);
            if (component == null)
                return new ErrorResponse($"'{go.name}' has no {typeName} component.");

            Undo.DestroyObjectImmediate(component);

            return new SuccessResponse($"Removed {typeName} from '{go.name}'.", new { removed = typeName });
        }

        static object Rename(ToolParams p)
        {
            var result = GameObjectResolver.Resolve(p);
            if (!result.IsSuccess) return new ErrorResponse(result.ErrorMessage);

            var newName = p.Get("new_name");
            if (string.IsNullOrEmpty(newName))
                return new ErrorResponse("'new_name' is required.");

            var go = result.Value;
            string oldName = go.name;
            Undo.RecordObject(go, "Rename");
            go.name = newName;

            return new SuccessResponse($"Renamed '{oldName}' → '{newName}'.", new
            {
                instance_id = go.GetInstanceID(),
                old_name = oldName,
                new_name = newName
            });
        }

        static object Duplicate(ToolParams p)
        {
            var result = GameObjectResolver.Resolve(p);
            if (!result.IsSuccess) return new ErrorResponse(result.ErrorMessage);

            var original = result.Value;
            var clone = UnityEngine.Object.Instantiate(original);
            clone.name = original.name; // Instantiate 시 (Clone) 접미사 제거
            clone.transform.SetParent(original.transform.parent, true);
            Undo.RegisterCreatedObjectUndo(clone, $"Duplicate {original.name}");

            return new SuccessResponse($"Duplicated '{original.name}'.", new
            {
                original_instance_id = original.GetInstanceID(),
                new_instance_id = clone.GetInstanceID(),
                name = clone.name
            });
        }

        static object SetParent(ToolParams p)
        {
            var result = GameObjectResolver.Resolve(p);
            if (!result.IsSuccess) return new ErrorResponse(result.ErrorMessage);

            var go = result.Value;
            var parent = GameObjectResolver.ResolveParent(p);
            // parent가 null이면 루트로 이동
            Undo.SetTransformParent(go.transform, parent?.transform, $"Set parent of {go.name}");

            string parentName = parent != null ? parent.name : "(root)";
            return new SuccessResponse($"Parent of '{go.name}' set to '{parentName}'.", new
            {
                instance_id = go.GetInstanceID(),
                parent = parent?.name
            });
        }

        // position/rotation/scale 적용 헬퍼
        static void ApplyTransform(GameObject go, ToolParams p)
        {
            var pos = GameObjectResolver.ParseVector3(p.GetRaw("position"));
            if (pos.HasValue) go.transform.localPosition = pos.Value;

            var rot = GameObjectResolver.ParseVector3(p.GetRaw("rotation"));
            if (rot.HasValue) go.transform.localEulerAngles = rot.Value;

            var scale = GameObjectResolver.ParseVector3(p.GetRaw("scale"));
            if (scale.HasValue) go.transform.localScale = scale.Value;
        }
    }
}
