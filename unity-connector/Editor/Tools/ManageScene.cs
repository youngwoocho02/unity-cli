using System.Collections.Generic;
using System.Linq;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEditor.SceneManagement;
using UnityEngine;
using UnityEngine.SceneManagement;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Name = "scene", Description = "Manage scenes. Actions: list, get_active, open, save, new, close, set_active.", Group = "scene")]
    public static class ManageScene
    {
        public class Parameters
        {
            [ToolParameter("Action: list, get_active, open, save, new, close, set_active", Required = true)]
            public string Action { get; set; }

            [ToolParameter("Scene asset path (e.g. Assets/Scenes/Main.unity)")]
            public string Path { get; set; }

            [ToolParameter("Scene name for new scene")]
            public string Name { get; set; }

            [ToolParameter("Open scene additively")]
            public bool Additive { get; set; }

            [ToolParameter("Save all open scenes")]
            public bool All { get; set; }

            [ToolParameter("Template for new scene: default or empty")]
            public string Template { get; set; }
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
                case "list": return ListScenes();
                case "get_active": return GetActive();
                case "open": return Open(p);
                case "save": return Save(p);
                case "new": return NewScene(p);
                case "close": return Close(p);
                case "set_active": return SetActive(p);
                default:
                    return new ErrorResponse($"Unknown action: '{actionResult.Value}'.");
            }
        }

        static object ListScenes()
        {
            var scenes = new List<object>();
            for (int i = 0; i < SceneManager.sceneCount; i++)
            {
                var scene = SceneManager.GetSceneAt(i);
                scenes.Add(BuildSceneInfo(scene));
            }
            return new SuccessResponse($"{scenes.Count} scenes loaded.", new { scenes });
        }

        static object GetActive()
        {
            var scene = SceneManager.GetActiveScene();
            return new SuccessResponse($"Active scene: {scene.name}.", BuildSceneInfo(scene));
        }

        static object Open(ToolParams p)
        {
            if (EditorApplication.isPlaying)
                return new ErrorResponse("Cannot open scene during play mode.");

            var pathResult = p.GetRequired("path", "'path' is required.");
            if (!pathResult.IsSuccess) return new ErrorResponse(pathResult.ErrorMessage);

            bool additive = p.GetBool("additive");
            var mode = additive ? OpenSceneMode.Additive : OpenSceneMode.Single;

            var scene = EditorSceneManager.OpenScene(pathResult.Value, mode);

            return new SuccessResponse($"Opened '{scene.name}'.", new
            {
                opened = scene.name,
                mode = additive ? "additive" : "single"
            });
        }

        static object Save(ToolParams p)
        {
            bool all = p.GetBool("all");
            string path = p.Get("path");

            if (all || string.IsNullOrEmpty(path))
            {
                EditorSceneManager.SaveOpenScenes();
                var saved = new List<string>();
                for (int i = 0; i < SceneManager.sceneCount; i++)
                    saved.Add(SceneManager.GetSceneAt(i).path);

                return new SuccessResponse("All scenes saved.", new { saved });
            }

            // 특정 씬 저장
            for (int i = 0; i < SceneManager.sceneCount; i++)
            {
                var scene = SceneManager.GetSceneAt(i);
                if (scene.path == path || scene.name == path)
                {
                    EditorSceneManager.SaveScene(scene);
                    return new SuccessResponse($"Saved '{scene.name}'.", new { saved = new[] { scene.path } });
                }
            }

            return new ErrorResponse($"Scene '{path}' not found among loaded scenes.");
        }

        static object NewScene(ToolParams p)
        {
            if (EditorApplication.isPlaying)
                return new ErrorResponse("Cannot create scene during play mode.");

            string template = p.Get("template", "default");
            var setup = template == "empty"
                ? NewSceneSetup.EmptyScene
                : NewSceneSetup.DefaultGameObjects;

            var scene = EditorSceneManager.NewScene(setup, NewSceneMode.Single);

            string name = p.Get("name");
            if (!string.IsNullOrEmpty(name))
            {
                string savePath = $"Assets/Scenes/{name}.unity";

                // Scenes 폴더가 없으면 생성
                if (!AssetDatabase.IsValidFolder("Assets/Scenes"))
                    AssetDatabase.CreateFolder("Assets", "Scenes");

                EditorSceneManager.SaveScene(scene, savePath);
                return new SuccessResponse($"Created and saved '{name}'.", new
                {
                    name,
                    path = savePath
                });
            }

            return new SuccessResponse("Created new scene.", new
            {
                name = scene.name,
                path = scene.path
            });
        }

        static object Close(ToolParams p)
        {
            if (EditorApplication.isPlaying)
                return new ErrorResponse("Cannot close scene during play mode.");

            var pathResult = p.GetRequired("path", "'path' is required.");
            if (!pathResult.IsSuccess) return new ErrorResponse(pathResult.ErrorMessage);

            for (int i = 0; i < SceneManager.sceneCount; i++)
            {
                var scene = SceneManager.GetSceneAt(i);
                if (scene.path == pathResult.Value || scene.name == pathResult.Value)
                {
                    if (SceneManager.sceneCount <= 1)
                        return new ErrorResponse("Cannot close the last remaining scene.");

                    EditorSceneManager.CloseScene(scene, true);
                    return new SuccessResponse($"Closed '{scene.name}'.", new { closed = scene.name });
                }
            }

            return new ErrorResponse($"Scene '{pathResult.Value}' not found.");
        }

        static object SetActive(ToolParams p)
        {
            var pathResult = p.GetRequired("path", "'path' is required.");
            if (!pathResult.IsSuccess) return new ErrorResponse(pathResult.ErrorMessage);

            for (int i = 0; i < SceneManager.sceneCount; i++)
            {
                var scene = SceneManager.GetSceneAt(i);
                if (scene.path == pathResult.Value || scene.name == pathResult.Value)
                {
                    SceneManager.SetActiveScene(scene);
                    return new SuccessResponse($"Active scene set to '{scene.name}'.", new
                    {
                        active_scene = scene.name
                    });
                }
            }

            return new ErrorResponse($"Scene '{pathResult.Value}' not found.");
        }

        static object BuildSceneInfo(Scene scene)
        {
            return new
            {
                name = scene.name,
                path = scene.path,
                build_index = scene.buildIndex,
                is_loaded = scene.isLoaded,
                is_dirty = scene.isDirty,
                root_count = scene.rootCount
            };
        }
    }
}
