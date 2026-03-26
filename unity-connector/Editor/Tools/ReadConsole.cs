using System;
using System.Collections.Generic;
using System.Linq;
using System.Reflection;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Name = "console", Description = "Read or clear Unity console logs.")]
    public static class ReadConsole
    {
        private static MethodInfo _startGettingEntriesMethod, _endGettingEntriesMethod, _clearMethod, _getCountMethod, _getEntryMethod;
        private static FieldInfo _modeField, _messageField, _fileField, _lineField;
        private static Type _logEntryType;

        static ReadConsole()
        {
            try
            {
                Type logEntriesType = typeof(EditorApplication).Assembly.GetType("UnityEditor.LogEntries");
                if (logEntriesType == null) throw new Exception("Could not find UnityEditor.LogEntries");
                BindingFlags sf = BindingFlags.Static | BindingFlags.Public | BindingFlags.NonPublic;
                BindingFlags inf = BindingFlags.Instance | BindingFlags.Public | BindingFlags.NonPublic;

                _startGettingEntriesMethod = logEntriesType.GetMethod("StartGettingEntries", sf)
                    ?? throw new Exception("Method not found: LogEntries.StartGettingEntries");
                _endGettingEntriesMethod = logEntriesType.GetMethod("EndGettingEntries", sf)
                    ?? throw new Exception("Method not found: LogEntries.EndGettingEntries");
                _clearMethod = logEntriesType.GetMethod("Clear", sf)
                    ?? throw new Exception("Method not found: LogEntries.Clear");
                _getCountMethod = logEntriesType.GetMethod("GetCount", sf)
                    ?? throw new Exception("Method not found: LogEntries.GetCount");
                _getEntryMethod = logEntriesType.GetMethod("GetEntryInternal", sf)
                    ?? throw new Exception("Method not found: LogEntries.GetEntryInternal");

                _logEntryType = typeof(EditorApplication).Assembly.GetType("UnityEditor.LogEntry")
                    ?? throw new Exception("Could not find UnityEditor.LogEntry");
                _modeField = _logEntryType.GetField("mode", inf)
                    ?? throw new Exception("Field not found: LogEntry.mode");
                _messageField = _logEntryType.GetField("message", inf)
                    ?? throw new Exception("Field not found: LogEntry.message");
                _fileField = _logEntryType.GetField("file", inf);
                _lineField = _logEntryType.GetField("line", inf);
            }
            catch (Exception e)
            {
                Debug.LogError($"[UnityCliConnector] ReadConsole init failed: {e.Message}");
                _startGettingEntriesMethod = _endGettingEntriesMethod = _clearMethod = _getCountMethod = _getEntryMethod = null;
                _modeField = _messageField = _fileField = _lineField = null;
                _logEntryType = null;
            }
        }

        public class Parameters
        {
            [ToolParameter("Filter: comma-separated log types (error, warning, log). Default: error,warning,log")]
            public string Filter { get; set; }

            [ToolParameter("Maximum number of log entries to return")]
            public int Lines { get; set; }

            [ToolParameter("Stack trace mode: none (first line), short (filtered), full (raw). Default: none")]
            public string Stacktrace { get; set; }

            [ToolParameter("Filter log messages containing this text")]
            public string FilterText { get; set; }

            [ToolParameter("Clear console")]
            public bool Clear { get; set; }
        }

        public static object HandleCommand(JObject @params)
        {
            if (_startGettingEntriesMethod == null || _endGettingEntriesMethod == null ||
                _clearMethod == null || _getCountMethod == null || _getEntryMethod == null ||
                _modeField == null || _messageField == null || _logEntryType == null)
                return new ErrorResponse("ReadConsole failed to initialize (reflection error).");

            if (@params == null)
                return new ErrorResponse("Parameters cannot be null.");

            var p = new ToolParams(@params);

            // --clear
            if (p.GetBool("clear") || p.Get("action", "").ToLower() == "clear")
            {
                _clearMethod.Invoke(null, null);
                return new SuccessResponse("Console cleared.");
            }

            var filter = p.Get("filter", "error,warning,log").ToLower();
            var types = filter.Split(',').Select(t => t.Trim()).Where(t => t.Length > 0).ToList();

            int? count = p.GetInt("lines") ?? p.GetInt("count");
            string filterText = p.Get("filter_text");
            string stacktrace = p.Get("stacktrace", "none").ToLower();

            return GetEntries(types, count, filterText, stacktrace);
        }

        private static object GetEntries(List<string> types, int? count, string filterText, string stacktrace)
        {
            var entries = new List<string>();
            try
            {
                _startGettingEntriesMethod.Invoke(null, null);
                int total = (int)_getCountMethod.Invoke(null, null);
                object logEntry = Activator.CreateInstance(_logEntryType);

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

                    entries.Add(FormatMessage(message, stacktrace));

                    if (count.HasValue && entries.Count >= count.Value) break;
                }
            }
            finally
            {
                try { _endGettingEntriesMethod.Invoke(null, null); } catch { }
            }

            return new SuccessResponse($"Retrieved {entries.Count} entries.", entries);
        }

        private static string FormatMessage(string message, string mode)
        {
            switch (mode)
            {
                case "full":
                    return message;

                case "short":
                    var lines = message.Split(new[] { '\n', '\r' }, StringSplitOptions.RemoveEmptyEntries);
                    var sb = new System.Text.StringBuilder();
                    foreach (var line in lines)
                    {
                        // Skip Unity/system internal frames
                        if (line.Contains("UnityEngine.Debug:") ||
                            line.Contains("UnityEditor.EditorGUIUtility:") ||
                            line.Contains("Unity.Entities.SystemState:") ||
                            line.Contains("(at Library/") ||
                            line.Contains("(at ./Library/"))
                            continue;
                        if (sb.Length > 0) sb.Append('\n');
                        sb.Append(line);
                    }
                    return sb.ToString();

                default: // "none"
                    string[] firstLine = message.Split(new[] { '\n', '\r' }, StringSplitOptions.RemoveEmptyEntries);
                    return firstLine.Length > 0 ? firstLine[0] : message;
            }
        }

        private const int ErrorMask =
            (1 << 0)  |  // Error
            (1 << 6)  |  // AssetImportError
            (1 << 8)  |  // ScriptingError
            (1 << 11) |  // ScriptCompileError
            (1 << 13);   // StickyError

        private const int WarningMask =
            (1 << 7)  |  // AssetImportWarning
            (1 << 9)  |  // ScriptingWarning
            (1 << 12);   // ScriptCompileWarning

        private const int ExceptionMask =
            (1 << 1)  |  // Assert
            (1 << 4)  |  // Fatal
            (1 << 17) |  // ScriptingException
            (1 << 21);   // ScriptingAssertion

        private static LogType GetLogTypeFromMode(int mode)
        {
            if ((mode & ExceptionMask) != 0) return LogType.Exception;
            if ((mode & ErrorMask) != 0) return LogType.Error;
            if ((mode & WarningMask) != 0) return LogType.Warning;
            return LogType.Log;
        }
    }
}
