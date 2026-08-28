using System;
using System.Linq;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEditorInternal;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Controls Unity editor state. Actions: play, stop, pause, set_active_tool, add_tag, remove_tag, add_layer, remove_layer.")]
    public static class ManageEditor
    {
        private const int FirstUserLayerIndex = 8;
        private const int TotalLayerCount = 32;

        public class Parameters
        {
            [ToolParameter("Action to perform: play, stop, pause, set_active_tool, add_tag, remove_tag, add_layer, remove_layer", Required = true)]
            public string Action { get; set; }

            [ToolParameter("Tool name (required for set_active_tool action)")]
            public string ToolName { get; set; }

            [ToolParameter("Tag name (required for add_tag/remove_tag actions)")]
            public string TagName { get; set; }

            [ToolParameter("Layer name (required for add_layer/remove_layer actions)")]
            public string LayerName { get; set; }
        }

        /// <summary>
        /// play/stop/pause는 즉시 플래그만 바꾼다. play 완료 대기는 CLI heartbeat 폴링이 한다.
        /// </summary>
        public static object HandleCommand(JObject @params)
        {
            if (@params == null)
                return new ErrorResponse("Parameters cannot be null.");

            var p = new ToolParams(@params);
            var actionResult = p.GetRequired("action");
            if (!actionResult.IsSuccess)
                return new ErrorResponse(actionResult.ErrorMessage);

            string action = actionResult.Value.ToLowerInvariant();

            switch (action)
            {
                case "play":
                    if (EditorApplication.isPlaying)
                        return new SuccessResponse("Already in play mode.");
                    // 실제 play 진입은 비동기. CLI --wait가 heartbeat의 playing을 확인한다.
                    EditorApplication.isPlaying = true;
                    return new SuccessResponse("Entered play mode.");

                case "pause":
                    if (!EditorApplication.isPlaying)
                        return new ErrorResponse("Cannot pause/resume: Not in play mode.");
                    EditorApplication.isPaused = !EditorApplication.isPaused;
                    return new SuccessResponse(EditorApplication.isPaused ? "Game paused." : "Game resumed.");

                case "stop":
                    if (!EditorApplication.isPlaying)
                        return new SuccessResponse("Already stopped (not in play mode).");
                    EditorApplication.isPlaying = false;
                    return new SuccessResponse("Exited play mode.");

                case "set_active_tool":
                    var toolNameResult = p.GetRequired("tool_name", "'tool_name' parameter required.");
                    if (!toolNameResult.IsSuccess) return new ErrorResponse(toolNameResult.ErrorMessage);
                    if (Enum.TryParse<Tool>(toolNameResult.Value, true, out var targetTool) && targetTool != Tool.None && targetTool <= Tool.Custom)
                    {
                        UnityEditor.Tools.current = targetTool;
                        return new SuccessResponse($"Set active tool to '{targetTool}'.");
                    }
                    return new ErrorResponse($"Could not parse '{toolNameResult.Value}' as a Unity Tool.");

                case "add_tag":
                    var addTagResult = p.GetRequired("tag_name", "'tag_name' parameter required.");
                    if (!addTagResult.IsSuccess) return new ErrorResponse(addTagResult.ErrorMessage);
                    if (InternalEditorUtility.tags.Contains(addTagResult.Value))
                        return new ErrorResponse($"Tag '{addTagResult.Value}' already exists.");
                    InternalEditorUtility.AddTag(addTagResult.Value);
                    AssetDatabase.SaveAssets();
                    return new SuccessResponse($"Tag '{addTagResult.Value}' added.");

                case "remove_tag":
                    var removeTagResult = p.GetRequired("tag_name", "'tag_name' parameter required.");
                    if (!removeTagResult.IsSuccess) return new ErrorResponse(removeTagResult.ErrorMessage);
                    if (!InternalEditorUtility.tags.Contains(removeTagResult.Value))
                        return new ErrorResponse($"Tag '{removeTagResult.Value}' does not exist.");
                    InternalEditorUtility.RemoveTag(removeTagResult.Value);
                    AssetDatabase.SaveAssets();
                    return new SuccessResponse($"Tag '{removeTagResult.Value}' removed.");

                case "add_layer":
                case "remove_layer":
                    return ManageLayer(action, p);

                default:
                    return new ErrorResponse($"Unknown action: '{action}'.");
            }
        }

        /// <summary>
        /// 유저 레이어(인덱스 8~31)만 추가/삭제한다. 빌트인 0~7은 건드리지 않는다.
        /// </summary>
        private static object ManageLayer(string action, ToolParams p)
        {
            var nameResult = p.GetRequired("layer_name", "'layer_name' parameter required.");
            if (!nameResult.IsSuccess) return new ErrorResponse(nameResult.ErrorMessage);

            var tagManagerAssets = AssetDatabase.LoadAllAssetsAtPath("ProjectSettings/TagManager.asset");
            if (tagManagerAssets == null || tagManagerAssets.Length == 0)
                return new ErrorResponse("Could not access TagManager asset.");

            using var tagManager = new SerializedObject(tagManagerAssets[0]);
            var layersProp = tagManager.FindProperty("layers");
            if (layersProp == null || !layersProp.isArray)
                return new ErrorResponse("Could not find 'layers' property.");

            if (action == "add_layer")
            {
                int firstEmpty = -1;
                for (int i = 0; i < TotalLayerCount; i++)
                {
                    var sp = layersProp.GetArrayElementAtIndex(i);
                    if (sp != null && nameResult.Value.Equals(sp.stringValue, StringComparison.OrdinalIgnoreCase))
                        return new ErrorResponse($"Layer '{nameResult.Value}' already exists at index {i}.");
                    if (firstEmpty == -1 && i >= FirstUserLayerIndex && (sp == null || string.IsNullOrEmpty(sp.stringValue)))
                        firstEmpty = i;
                }
                if (firstEmpty == -1) return new ErrorResponse("No empty layer slots available.");
                layersProp.GetArrayElementAtIndex(firstEmpty).stringValue = nameResult.Value;
                tagManager.ApplyModifiedProperties();
                AssetDatabase.SaveAssets();
                return new SuccessResponse($"Layer '{nameResult.Value}' added to slot {firstEmpty}.");
            }

            for (int i = FirstUserLayerIndex; i < TotalLayerCount; i++)
            {
                var sp = layersProp.GetArrayElementAtIndex(i);
                if (sp != null && nameResult.Value.Equals(sp.stringValue, StringComparison.OrdinalIgnoreCase))
                {
                    sp.stringValue = string.Empty;
                    tagManager.ApplyModifiedProperties();
                    AssetDatabase.SaveAssets();
                    return new SuccessResponse($"Layer '{nameResult.Value}' removed from slot {i}.");
                }
            }
            return new ErrorResponse($"User layer '{nameResult.Value}' not found.");
        }
    }
}
