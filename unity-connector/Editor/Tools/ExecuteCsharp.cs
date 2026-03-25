using System;
using System.Collections;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Text;
using System.Text.RegularExpressions;
using Newtonsoft.Json.Linq;
using UnityEditor;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Name = "exec", Description = "Execute arbitrary C# code at runtime. Full access to Unity and all loaded assemblies.")]
    public static class ExecuteCsharp
    {
        private static readonly string[] DefaultUsings =
        {
            "System",
            "System.Collections.Generic",
            "System.IO",
            "System.Linq",
            "System.Reflection",
            "System.Threading.Tasks",
            "UnityEngine",
            "UnityEngine.SceneManagement",
            "UnityEditor",
            "UnityEditor.SceneManagement",
            "UnityEditorInternal",
        };

        public class Parameters
        {
            [ToolParameter("C# code to execute. Use 'return' for output.", Required = true)]
            public string Code { get; set; }

            [ToolParameter("Additional using directives (comma-separated, e.g. Unity.Entities,Unity.Mathematics)")]
            public string[] Usings { get; set; }
        }

        public static object HandleCommand(JObject parameters)
        {
            var p = new ToolParams(parameters);
            var code = p.Get("code")
                ?? (p.GetRaw("args") as JArray)?[0]?.ToString();
            if (string.IsNullOrEmpty(code))
                return new ErrorResponse("'code' required");

            var usingsToken = p.GetRaw("usings");
            var extraUsings = new List<string>();
            if (usingsToken != null)
            {
                if (usingsToken.Type == JTokenType.Array)
                    extraUsings.AddRange(usingsToken.ToObject<string[]>());
                else
                    extraUsings.AddRange(usingsToken.ToString().Split(','));
            }

            return CompileAndExecute(BuildSource(code, extraUsings));
        }

        private static string BuildSource(string code, List<string> extraUsings)
        {
            var sb = new StringBuilder();
            foreach (var u in DefaultUsings)
                sb.AppendLine($"using {u};");
            foreach (var u in extraUsings)
                sb.AppendLine($"using {u};");

            sb.AppendLine();
            sb.AppendLine("public static class __CliDynamic {");
            sb.AppendLine("  public static object Execute() {");
            sb.AppendLine(code);
            sb.AppendLine("  }");
            sb.AppendLine("}");
            return sb.ToString();
        }

        private static object CompileAndExecute(string source)
        {
            var utf8 = new UTF8Encoding(false);
            var tmpDir = Path.Combine(Path.GetTempPath(), "unity-cli-exec");
            Directory.CreateDirectory(tmpDir);

            var id = Guid.NewGuid().ToString("N").Substring(0, 8);
            var srcFile = Path.Combine(tmpDir, $"{id}.cs");
            var outFile = Path.Combine(tmpDir, $"{id}.dll");
            var rspFile = Path.Combine(tmpDir, $"{id}.rsp");

            try
            {
                File.WriteAllText(srcFile, source, utf8);

                var rsp = new StringBuilder();
                rsp.AppendLine("-target:library");
                rsp.AppendLine($"-out:\"{outFile}\"");
                rsp.AppendLine("-nologo");
                rsp.AppendLine("-nowarn:0105,1701,1702");
                rsp.AppendLine("-langversion:latest");
                rsp.AppendLine($"\"{srcFile}\"");

                var added = new HashSet<string>();
                foreach (var asm in AppDomain.CurrentDomain.GetAssemblies())
                {
                    try
                    {
                        if (asm.IsDynamic || string.IsNullOrEmpty(asm.Location)) continue;
                        if (!added.Add(asm.GetName().Name)) continue;
                        rsp.AppendLine($"-r:\"{asm.Location}\"");
                    }
                    catch { }
                }

                File.WriteAllText(rspFile, rsp.ToString(), utf8);

                var (exe, args) = FindCsc(rspFile);
                if (exe == null)
                    return new ErrorResponse(
                        "Cannot find csc compiler. Searched in: DotNetSdkRoslyn, MonoBleedingEdge/bin, MonoBleedingEdge/bin-linux64");

                var psi = new ProcessStartInfo
                {
                    FileName = exe,
                    Arguments = args,
                    UseShellExecute = false,
                    RedirectStandardOutput = true,
                    RedirectStandardError = true,
                    CreateNoWindow = true,
                    StandardOutputEncoding = Encoding.UTF8,
                    StandardErrorEncoding = Encoding.UTF8,
                };

                using (var proc = Process.Start(psi))
                {
                    var stdout = proc.StandardOutput.ReadToEnd();
                    var stderr = proc.StandardError.ReadToEnd();
                    proc.WaitForExit(30000);

                    if (proc.ExitCode != 0)
                    {
                        var output = string.IsNullOrEmpty(stderr) ? stdout : stderr;
                        return new ErrorResponse($"Compile error:\n{FormatErrors(output)}");
                    }
                }

                var bytes = File.ReadAllBytes(outFile);
                var compiled = Assembly.Load(bytes);
                var method = compiled.GetType("__CliDynamic")?.GetMethod("Execute");
                if (method == null)
                    return new ErrorResponse("Internal error: compiled type or method not found.");

                object result;
                try
                {
                    result = method.Invoke(null, null);
                }
                catch (TargetInvocationException tie)
                {
                    var inner = tie.InnerException ?? tie;
                    return new ErrorResponse($"Runtime error: {inner.GetType().Name}: {inner.Message}");
                }
                return new SuccessResponse("OK", Serialize(result, 0));
            }
            finally
            {
                try { File.Delete(srcFile); } catch { }
                try { File.Delete(outFile); } catch { }
                try { File.Delete(rspFile); } catch { }
            }
        }

        private static (string exe, string args) FindCsc(string rspFile)
        {
            var content = EditorApplication.applicationContentsPath;
            var roslynDir = Path.Combine(content, "DotNetSdkRoslyn");
            var rspArg = $"@\"{rspFile}\"";

            // Try DotNetSdkRoslyn first (newer Unity versions)
            if (Application.platform == RuntimePlatform.WindowsEditor)
            {
                var cscExe = Path.Combine(roslynDir, "csc.exe");
                if (File.Exists(cscExe))
                    return (cscExe, rspArg);
            }

            var cscDll = Path.Combine(roslynDir, "csc.dll");
            if (File.Exists(cscDll))
            {
                var dotnet = FindDotnet();
                if (dotnet != null)
                    return (dotnet, $"exec \"{cscDll}\" {rspArg}");
            }

            // Fallback to MonoBleedingEdge (older Unity versions, Unity 6000.x)
            var monoBleedingEdge = Path.Combine(content, "MonoBleedingEdge");

            // Use mono to execute csc.exe directly (wrapper scripts have broken paths)
            string monoBin = null;
            if (Application.platform == RuntimePlatform.WindowsEditor)
                monoBin = Path.Combine(monoBleedingEdge, "bin", "mono.exe");
            else if (Application.platform == RuntimePlatform.LinuxEditor)
                monoBin = Path.Combine(monoBleedingEdge, "bin-linux64", "mono");
            else if (Application.platform == RuntimePlatform.OSXEditor)
                monoBin = Path.Combine(monoBleedingEdge, "bin", "mono");

            if (monoBin != null && File.Exists(monoBin))
            {
                var cscExe = Path.Combine(monoBleedingEdge, "lib", "mono", "4.5", "csc.exe");
                if (File.Exists(cscExe))
                    return (monoBin, $"\"{cscExe}\" {rspArg}");
            }

            return (null, null);
        }

        private static string FindDotnet()
        {
            var content = EditorApplication.applicationContentsPath;
            var ext = Application.platform == RuntimePlatform.WindowsEditor ? ".exe" : "";

            var candidates = new[]
            {
                Path.Combine(content, "NetCoreRuntime", $"dotnet{ext}"),
                Path.Combine(content, "DotNetRuntimeDownloaded", $"dotnet{ext}"),
                $"dotnet{ext}",
            };

            foreach (var c in candidates)
                if (File.Exists(c) || c == candidates[candidates.Length - 1])
                    return c;

            return null;
        }

        private static string FormatErrors(string raw)
        {
            var lines = raw.Split('\n');
            var errors = new List<string>();
            foreach (var line in lines)
            {
                var trimmed = line.Trim();
                if (string.IsNullOrEmpty(trimmed)) continue;
                var m = Regex.Match(trimmed, @"\((\d+),\d+\):\s*error\s+\w+:\s*(.+)");
                if (m.Success)
                    errors.Add($"L{m.Groups[1].Value}: {m.Groups[2].Value}");
                else if (trimmed.Contains("error"))
                    errors.Add(trimmed);
            }
            return errors.Count > 0 ? string.Join("\n", errors) : raw;
        }

        private static object Serialize(object obj, int depth)
        {
            if (obj == null) return null;
            if (depth > 4) return obj.ToString();
            var type = obj.GetType();
            if (type.IsPrimitive || type == typeof(string) || type == typeof(decimal)) return obj;
            if (type.IsEnum) return obj.ToString();
            if (type.Name.StartsWith("FixedString")) return obj.ToString();
            if (obj is IDictionary dict)
            {
                var r = new Dictionary<string, object>();
                foreach (DictionaryEntry e in dict)
                    r[e.Key.ToString()] = Serialize(e.Value, depth + 1);
                return r;
            }
            if (obj is IEnumerable enumerable)
            {
                var list = new List<object>();
                int count = 0;
                foreach (var item in enumerable)
                {
                    if (count++ >= 100) { list.Add("... (truncated at 100)"); break; }
                    list.Add(Serialize(item, depth + 1));
                }
                return list;
            }
            if (type.IsValueType || type.IsClass)
            {
                var fields = type.GetFields(BindingFlags.Public | BindingFlags.Instance);
                if (fields.Length > 0)
                {
                    var r = new Dictionary<string, object>();
                    foreach (var f in fields)
                    {
                        try { r[f.Name] = Serialize(f.GetValue(obj), depth + 1); }
                        catch { r[f.Name] = "<error>"; }
                    }
                    return r;
                }
            }
            return obj.ToString();
        }
    }
}
