using System;
using System.Collections.Generic;
using System.Linq;
using System.Reflection;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Read or clear Unity console logs.")]
    public static class ReadConsole
    {
        private static MethodInfo _startGettingEntriesMethod, _endGettingEntriesMethod, _clearMethod, _getCountMethod, _getEntryMethod;
        private static FieldInfo _modeField, _messageField, _fileField, _lineField;

        static ReadConsole()
        {
            try
            {
                Type logEntriesType = typeof(EditorApplication).Assembly.GetType("UnityEditor.LogEntries");
                if (logEntriesType == null) throw new Exception("Could not find UnityEditor.LogEntries");
                BindingFlags sf = BindingFlags.Static | BindingFlags.Public | BindingFlags.NonPublic;
                BindingFlags inf = BindingFlags.Instance | BindingFlags.Public | BindingFlags.NonPublic;

                _startGettingEntriesMethod = logEntriesType.GetMethod("StartGettingEntries", sf);
                _endGettingEntriesMethod = logEntriesType.GetMethod("EndGettingEntries", sf);
                _clearMethod = logEntriesType.GetMethod("Clear", sf);
                _getCountMethod = logEntriesType.GetMethod("GetCount", sf);
                _getEntryMethod = logEntriesType.GetMethod("GetEntryInternal", sf);

                Type logEntryType = typeof(EditorApplication).Assembly.GetType("UnityEditor.LogEntry");
                _modeField = logEntryType.GetField("mode", inf);
                _messageField = logEntryType.GetField("message", inf);
                _fileField = logEntryType.GetField("file", inf);
                _lineField = logEntryType.GetField("line", inf);
            }
            catch (Exception e)
            {
                Debug.LogError($"[UnityCliConnector] ReadConsole init failed: {e.Message}");
                _startGettingEntriesMethod = _endGettingEntriesMethod = _clearMethod = _getCountMethod = _getEntryMethod = null;
                _modeField = _messageField = _fileField = _lineField = null;
            }
        }

        public static object HandleCommand(JObject @params)
        {
            if (_startGettingEntriesMethod == null || _getEntryMethod == null)
                return new ErrorResponse("ReadConsole failed to initialize (reflection error).");

            if (@params == null)
                return new ErrorResponse("Parameters cannot be null.");

            var p = new ToolParams(@params);
            string action = p.Get("action", "get").ToLower();

            if (action == "clear")
            {
                _clearMethod.Invoke(null, null);
                return new SuccessResponse("Console cleared.");
            }

            if (action == "get")
            {
                var types = (p.GetRaw("types") as JArray)?.Select(t => t.ToString().ToLower()).ToList()
                    ?? new List<string> { "error", "warning" };
                int? count = p.GetInt("count");
                string filterText = p.Get("filterText");
                if (types.Contains("all")) types = new List<string> { "error", "warning", "log" };

                return GetEntries(types, count, filterText);
            }

            return new ErrorResponse($"Unknown action: '{action}'. Valid: get, clear.");
        }

        private static object GetEntries(List<string> types, int? count, string filterText)
        {
            var entries = new List<string>();
            try
            {
                _startGettingEntriesMethod.Invoke(null, null);
                int total = (int)_getCountMethod.Invoke(null, null);
                Type logEntryType = typeof(EditorApplication).Assembly.GetType("UnityEditor.LogEntry");
                object logEntry = Activator.CreateInstance(logEntryType);

                for (int i = 0; i < total; i++)
                {
                    _getEntryMethod.Invoke(null, new object[] { i, logEntry });
                    int mode = (int)_modeField.GetValue(logEntry);
                    string message = (string)_messageField.GetValue(logEntry);
                    if (string.IsNullOrEmpty(message)) continue;

                    LogType logType = GetLogTypeFromMode(mode);
                    bool want = logType == LogType.Exception || logType == LogType.Assert
                        ? types.Contains("error")
                        : types.Contains(logType.ToString().ToLowerInvariant());

                    if (!want) continue;
                    if (!string.IsNullOrEmpty(filterText) && message.IndexOf(filterText, StringComparison.OrdinalIgnoreCase) < 0) continue;

                    string[] lines = message.Split(new[] { '\n', '\r' }, StringSplitOptions.RemoveEmptyEntries);
                    entries.Add(lines.Length > 0 ? lines[0] : message);

                    if (count.HasValue && entries.Count >= count.Value) break;
                }
            }
            finally
            {
                try { _endGettingEntriesMethod.Invoke(null, null); } catch { }
            }

            return new SuccessResponse($"Retrieved {entries.Count} entries.", entries);
        }

        private const int ModeBitError = 1, ModeBitWarning = 4, ModeBitException = 16,
            ModeBitScriptingError = 512, ModeBitScriptingWarning = 1024, ModeBitScriptingException = 262144;

        private static LogType GetLogTypeFromMode(int mode)
        {
            if ((mode & (ModeBitException | ModeBitScriptingException)) != 0) return LogType.Exception;
            if ((mode & (ModeBitError | ModeBitScriptingError)) != 0) return LogType.Error;
            if ((mode & (ModeBitWarning | ModeBitScriptingWarning)) != 0) return LogType.Warning;
            return LogType.Log;
        }
    }
}
