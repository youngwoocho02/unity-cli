using System;
using System.Collections.Generic;
using System.Globalization;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Name = "component", Description = "Manage component properties. Actions: get_properties, set_property.", Group = "scene")]
    public static class ManageComponent
    {
        public class Parameters
        {
            [ToolParameter("Action: get_properties, set_property", Required = true)]
            public string Action { get; set; }

            [ToolParameter("Target GameObject instance ID")]
            public int InstanceId { get; set; }

            [ToolParameter("Target GameObject path or name")]
            public string Path { get; set; }

            [ToolParameter("Target GameObject name")]
            public string Name { get; set; }

            [ToolParameter("Component type name", Required = true)]
            public string ComponentType { get; set; }

            [ToolParameter("Property name (SerializedProperty path)")]
            public string Property { get; set; }

            [ToolParameter("Property value to set")]
            public string Value { get; set; }

            [ToolParameter("Max properties to return (default 50)")]
            public int PageSize { get; set; }
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
                case "get_properties": return GetProperties(p);
                case "set_property": return SetProperty(p);
                default:
                    return new ErrorResponse($"Unknown action: '{actionResult.Value}'.");
            }
        }

        static object GetProperties(ToolParams p)
        {
            var resolved = ResolveComponent(p);
            if (resolved.error != null) return resolved.error;

            int pageSize = p.GetInt("page_size") ?? 50;
            var so = new SerializedObject(resolved.component);
            var props = new List<object>();
            var iter = so.GetIterator();

            if (iter.NextVisible(true))
            {
                do
                {
                    // m_Script 필드 스킵
                    if (iter.name == "m_Script") continue;
                    // depth 0~1만 수집 (깊은 구조체 회피)
                    if (iter.depth > 1) continue;

                    props.Add(new
                    {
                        name = iter.name,
                        display_name = iter.displayName,
                        type = iter.propertyType.ToString(),
                        value = ReadPropertyValue(iter)
                    });

                    if (props.Count >= pageSize) break;
                }
                while (iter.NextVisible(false));
            }

            so.Dispose();

            return new SuccessResponse($"{props.Count} properties on {resolved.typeName}.", new
            {
                instance_id = resolved.go.GetInstanceID(),
                component_type = resolved.typeName,
                properties = props
            });
        }

        static object SetProperty(ToolParams p)
        {
            var resolved = ResolveComponent(p);
            if (resolved.error != null) return resolved.error;

            var propName = p.Get("property");
            if (string.IsNullOrEmpty(propName))
                return new ErrorResponse("'property' is required.");

            var valueStr = p.Get("value");
            var valueToken = p.GetRaw("value");

            var so = new SerializedObject(resolved.component);
            var prop = so.FindProperty(propName);
            if (prop == null)
            {
                so.Dispose();
                return new ErrorResponse($"Property '{propName}' not found on {resolved.typeName}.");
            }

            string error = WritePropertyValue(prop, valueStr, valueToken);
            if (error != null)
            {
                so.Dispose();
                return new ErrorResponse(error);
            }

            so.ApplyModifiedProperties();
            so.Dispose();

            return new SuccessResponse($"Set {resolved.typeName}.{propName}.", new
            {
                instance_id = resolved.go.GetInstanceID(),
                component_type = resolved.typeName,
                property = propName,
                value = valueStr
            });
        }

        // 컴포넌트 해석 헬퍼
        struct ResolvedComponent
        {
            public GameObject go;
            public Component component;
            public string typeName;
            public ErrorResponse error;
        }

        static ResolvedComponent ResolveComponent(ToolParams p)
        {
            var goResult = GameObjectResolver.Resolve(p);
            if (!goResult.IsSuccess)
                return new ResolvedComponent { error = new ErrorResponse(goResult.ErrorMessage) };

            var typeName = p.Get("component_type");
            if (string.IsNullOrEmpty(typeName))
                return new ResolvedComponent { error = new ErrorResponse("'component_type' is required.") };

            var type = GameObjectResolver.ResolveComponentType(typeName);
            if (type == null)
                return new ResolvedComponent { error = new ErrorResponse($"Component type '{typeName}' not found.") };

            var component = goResult.Value.GetComponent(type);
            if (component == null)
                return new ResolvedComponent { error = new ErrorResponse($"'{goResult.Value.name}' has no {typeName} component.") };

            return new ResolvedComponent
            {
                go = goResult.Value,
                component = component,
                typeName = type.Name
            };
        }

        // SerializedProperty 값 읽기
        static object ReadPropertyValue(SerializedProperty prop)
        {
            switch (prop.propertyType)
            {
                case SerializedPropertyType.Integer: return prop.intValue;
                case SerializedPropertyType.Boolean: return prop.boolValue;
                case SerializedPropertyType.Float: return prop.floatValue;
                case SerializedPropertyType.String: return prop.stringValue;
                case SerializedPropertyType.Enum: return prop.enumNames[prop.enumValueIndex];
                case SerializedPropertyType.Color:
                    var c = prop.colorValue;
                    return new[] { c.r, c.g, c.b, c.a };
                case SerializedPropertyType.Vector2:
                    var v2 = prop.vector2Value;
                    return new[] { v2.x, v2.y };
                case SerializedPropertyType.Vector3:
                    var v3 = prop.vector3Value;
                    return new[] { v3.x, v3.y, v3.z };
                case SerializedPropertyType.Vector4:
                    var v4 = prop.vector4Value;
                    return new[] { v4.x, v4.y, v4.z, v4.w };
                case SerializedPropertyType.Rect:
                    var r = prop.rectValue;
                    return new { x = r.x, y = r.y, width = r.width, height = r.height };
                case SerializedPropertyType.Bounds:
                    var b = prop.boundsValue;
                    return new
                    {
                        center = new[] { b.center.x, b.center.y, b.center.z },
                        size = new[] { b.size.x, b.size.y, b.size.z }
                    };
                case SerializedPropertyType.ObjectReference:
                    var obj = prop.objectReferenceValue;
                    return obj != null ? obj.name : null;
                case SerializedPropertyType.LayerMask:
                    return prop.intValue;
                default:
                    return $"({prop.propertyType})";
            }
        }

        // SerializedProperty 값 쓰기
        static string WritePropertyValue(SerializedProperty prop, string valueStr, JToken valueToken)
        {
            try
            {
                switch (prop.propertyType)
                {
                    case SerializedPropertyType.Integer:
                        if (!int.TryParse(valueStr, out int intVal))
                            return $"Cannot parse '{valueStr}' as integer.";
                        prop.intValue = intVal;
                        break;

                    case SerializedPropertyType.Boolean:
                        prop.boolValue = ParamCoercion.CoerceBool(valueToken, false);
                        break;

                    case SerializedPropertyType.Float:
                        if (!float.TryParse(valueStr, NumberStyles.Float, CultureInfo.InvariantCulture, out float floatVal))
                            return $"Cannot parse '{valueStr}' as float.";
                        prop.floatValue = floatVal;
                        break;

                    case SerializedPropertyType.String:
                        prop.stringValue = valueStr ?? "";
                        break;

                    case SerializedPropertyType.Enum:
                        // 이름 또는 인덱스로 설정
                        if (int.TryParse(valueStr, out int enumIdx))
                        {
                            prop.enumValueIndex = enumIdx;
                        }
                        else
                        {
                            int idx = Array.IndexOf(prop.enumNames, valueStr);
                            if (idx < 0)
                                return $"Enum value '{valueStr}' not found. Options: {string.Join(", ", prop.enumNames)}";
                            prop.enumValueIndex = idx;
                        }
                        break;

                    case SerializedPropertyType.Vector3:
                        var v3 = GameObjectResolver.ParseVector3(valueToken);
                        if (!v3.HasValue)
                            return $"Cannot parse '{valueStr}' as Vector3. Use 'x,y,z' or [x,y,z].";
                        prop.vector3Value = v3.Value;
                        break;

                    case SerializedPropertyType.Vector2:
                        var v2Token = valueToken;
                        if (v2Token != null && v2Token.Type == JTokenType.String)
                        {
                            var parts = valueStr.Split(',');
                            if (parts.Length >= 2 &&
                                float.TryParse(parts[0].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out float x2) &&
                                float.TryParse(parts[1].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out float y2))
                            {
                                prop.vector2Value = new Vector2(x2, y2);
                                break;
                            }
                        }
                        return $"Cannot parse '{valueStr}' as Vector2. Use 'x,y'.";

                    case SerializedPropertyType.Color:
                        var colorParts = valueStr?.Split(',');
                        if (colorParts != null && colorParts.Length >= 3)
                        {
                            float.TryParse(colorParts[0].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out float cr);
                            float.TryParse(colorParts[1].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out float cg);
                            float.TryParse(colorParts[2].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out float cb);
                            float ca = 1f;
                            if (colorParts.Length >= 4)
                                float.TryParse(colorParts[3].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out ca);
                            prop.colorValue = new Color(cr, cg, cb, ca);
                            break;
                        }
                        return $"Cannot parse '{valueStr}' as Color. Use 'r,g,b' or 'r,g,b,a'.";

                    case SerializedPropertyType.ObjectReference:
                        if (string.IsNullOrEmpty(valueStr))
                        {
                            prop.objectReferenceValue = null;
                        }
                        else
                        {
                            var asset = AssetDatabase.LoadAssetAtPath<UnityEngine.Object>(valueStr);
                            if (asset == null)
                                return $"Asset not found at '{valueStr}'.";
                            prop.objectReferenceValue = asset;
                        }
                        break;

                    case SerializedPropertyType.LayerMask:
                        if (!int.TryParse(valueStr, out int maskVal))
                            return $"Cannot parse '{valueStr}' as LayerMask integer.";
                        prop.intValue = maskVal;
                        break;

                    default:
                        return $"Property type '{prop.propertyType}' is not supported for set_property.";
                }
            }
            catch (Exception ex)
            {
                return $"Failed to set property: {ex.Message}";
            }

            return null; // 성공
        }
    }
}
