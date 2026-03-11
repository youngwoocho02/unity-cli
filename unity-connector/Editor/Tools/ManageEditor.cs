using System;
using System.Linq;
using System.Threading.Tasks;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEditorInternal;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Controls Unity editor state. Actions: play, pause, stop, set_active_tool, add_tag, remove_tag, add_layer, remove_layer.")]
    public static class ManageEditor
    {
        private const int FirstUserLayerIndex = 8;
        private const int TotalLayerCount = 32;
        private const int PlayModeTimeoutSeconds = 60;

        public static async Task<object> HandleCommand(JObject @params)
        {
            if (@params == null)
                return new ErrorResponse("Parameters cannot be null.");

            var p = new ToolParams(@params);
            var actionResult = p.GetRequired("action");
            if (!actionResult.IsSuccess)
                return new ErrorResponse(actionResult.ErrorMessage);

            string action = actionResult.Value.ToLowerInvariant();
            bool waitForCompletion = ParamCoercion.CoerceBool(@params?["waitForCompletion"] ?? @params?["wait_for_completion"], false);

            switch (action)
            {
                case "play":
                    if (!EditorApplication.isPlaying)
                    {
                        EditorApplication.isPlaying = true;
                        if (waitForCompletion)
                        {
                            await WaitForPlayModeStateAsync(PlayModeStateChange.EnteredPlayMode, TimeSpan.FromSeconds(PlayModeTimeoutSeconds));
                            return new SuccessResponse("Entered play mode (confirmed).");
                        }
                        return new SuccessResponse("Entered play mode.");
                    }
                    return new SuccessResponse("Already in play mode.");

                case "pause":
                    if (EditorApplication.isPlaying)
                    {
                        EditorApplication.isPaused = !EditorApplication.isPaused;
                        return new SuccessResponse(EditorApplication.isPaused ? "Game paused." : "Game resumed.");
                    }
                    return new ErrorResponse("Cannot pause/resume: Not in play mode.");

                case "stop":
                    if (EditorApplication.isPlaying)
                    {
                        EditorApplication.isPlaying = false;
                        if (waitForCompletion)
                        {
                            await WaitForPlayModeStateAsync(PlayModeStateChange.EnteredEditMode, TimeSpan.FromSeconds(PlayModeTimeoutSeconds));
                            return new SuccessResponse("Exited play mode (confirmed).");
                        }
                        return new SuccessResponse("Exited play mode.");
                    }
                    return new SuccessResponse("Already stopped (not in play mode).");

                case "set_active_tool":
                    var toolNameResult = p.GetRequired("toolName", "'toolName' parameter required.");
                    if (!toolNameResult.IsSuccess) return new ErrorResponse(toolNameResult.ErrorMessage);
                    if (Enum.TryParse<Tool>(toolNameResult.Value, true, out var targetTool) && targetTool != Tool.None && targetTool <= Tool.Custom)
                    {
                        UnityEditor.Tools.current = targetTool;
                        return new SuccessResponse($"Set active tool to '{targetTool}'.");
                    }
                    return new ErrorResponse($"Could not parse '{toolNameResult.Value}' as a Unity Tool.");

                case "add_tag":
                    var addTagResult = p.GetRequired("tagName", "'tagName' parameter required.");
                    if (!addTagResult.IsSuccess) return new ErrorResponse(addTagResult.ErrorMessage);
                    if (InternalEditorUtility.tags.Contains(addTagResult.Value))
                        return new ErrorResponse($"Tag '{addTagResult.Value}' already exists.");
                    InternalEditorUtility.AddTag(addTagResult.Value);
                    AssetDatabase.SaveAssets();
                    return new SuccessResponse($"Tag '{addTagResult.Value}' added.");

                case "remove_tag":
                    var removeTagResult = p.GetRequired("tagName", "'tagName' parameter required.");
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

        private static object ManageLayer(string action, ToolParams p)
        {
            var nameResult = p.GetRequired("layerName", "'layerName' parameter required.");
            if (!nameResult.IsSuccess) return new ErrorResponse(nameResult.ErrorMessage);

            var tagManagerAssets = AssetDatabase.LoadAllAssetsAtPath("ProjectSettings/TagManager.asset");
            if (tagManagerAssets == null || tagManagerAssets.Length == 0)
                return new ErrorResponse("Could not access TagManager asset.");

            var tagManager = new SerializedObject(tagManagerAssets[0]);
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
            else
            {
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

        private static Task WaitForPlayModeStateAsync(PlayModeStateChange targetState, TimeSpan timeout)
        {
            var tcs = new TaskCompletionSource<bool>(TaskCreationOptions.RunContinuationsAsynchronously);
            var start = DateTime.UtcNow;

            void OnStateChanged(PlayModeStateChange state)
            {
                if (state == targetState)
                {
                    EditorApplication.playModeStateChanged -= OnStateChanged;
                    tcs.TrySetResult(true);
                }
            }

            void Tick()
            {
                if (tcs.Task.IsCompleted) { EditorApplication.update -= Tick; return; }
                if ((DateTime.UtcNow - start) > timeout)
                {
                    EditorApplication.playModeStateChanged -= OnStateChanged;
                    EditorApplication.update -= Tick;
                    tcs.TrySetException(new TimeoutException());
                }
            }

            EditorApplication.playModeStateChanged += OnStateChanged;
            EditorApplication.update += Tick;
            return tcs.Task;
        }
    }
}
