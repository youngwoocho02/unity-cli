using System;
using System.Collections.Generic;
using Newtonsoft.Json.Linq;
using UnityEditor;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Execute a Unity menu item by path.")]
    public static class ExecuteMenuItem
    {
        private static readonly HashSet<string> Blacklist = new HashSet<string>(StringComparer.OrdinalIgnoreCase) { "File/Quit" };

        public static object HandleCommand(JObject @params)
        {
            string menuPath = @params["menu_path"]?.ToString() ?? @params["menuPath"]?.ToString();
            if (string.IsNullOrWhiteSpace(menuPath))
                return new ErrorResponse("'menu_path' parameter required.");

            if (Blacklist.Contains(menuPath))
                return new ErrorResponse($"Execution of '{menuPath}' is blocked for safety.");

            bool executed = EditorApplication.ExecuteMenuItem(menuPath);
            if (!executed)
                return new ErrorResponse($"Failed to execute menu item '{menuPath}'.");

            return new SuccessResponse($"Executed menu item: '{menuPath}'.");
        }
    }
}
