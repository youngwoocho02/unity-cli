using System;
using System.CodeDom.Compiler;
using System.Collections;
using System.Collections.Generic;
using System.Reflection;
using System.Text;
using System.Text.RegularExpressions;
using Microsoft.CSharp;
using Newtonsoft.Json.Linq;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Execute C# code at runtime. Dangerous namespaces (Process, Net, InteropServices, Win32) are blocked.")]
    public static class ExecuteCsharp
    {
        private static readonly string[] DefaultUsings =
        {
            "System",
            "System.Collections.Generic",
            "System.Linq",
            "UnityEngine",
            "UnityEditor",
        };

        private static readonly string[] BlockedNamespacePrefixes =
        {
            "System.Diagnostics.Process",
            "System.Net",
            "System.Runtime.InteropServices",
            "Microsoft.Win32",
        };

        private static readonly string[] BlockedSourcePatterns =
        {
            "System.Diagnostics.Process",
            "System.IO.File",
            "System.IO.Directory",
            "System.IO.StreamWriter",
            "System.IO.FileStream",
            "System.Net.WebClient",
            "System.Net.Http",
            "System.Net.Sockets",
            "System.Runtime.InteropServices.Marshal",
            "System.Runtime.InteropServices.DllImport",
            "Microsoft.Win32.Registry",
        };

        public class Parameters
        {
            [ToolParameter("C# code to execute. Single expressions auto-return their result", Required = true)]
            public string Code { get; set; }

            [ToolParameter("Additional using directives (e.g. Unity.Entities, Unity.Mathematics)")]
            public string[] Usings { get; set; }
        }

        public static object HandleCommand(JObject parameters)
        {
            var code = parameters["code"]?.Value<string>();
            if (string.IsNullOrEmpty(code))
                return new ErrorResponse("'code' required");

            var extraUsings = parameters["usings"]?.ToObject<string[]>();

            // Validate extra usings against blocklist
            if (extraUsings != null)
            {
                foreach (var u in extraUsings)
                {
                    foreach (var blocked in BlockedNamespacePrefixes)
                    {
                        if (u.StartsWith(blocked, StringComparison.OrdinalIgnoreCase))
                            return new ErrorResponse($"Blocked using directive: '{u}'");
                    }
                }
            }

            // Source-level scan for blocked fully-qualified type references
            foreach (var pattern in BlockedSourcePatterns)
            {
                if (code.Contains(pattern))
                    return new ErrorResponse($"Blocked reference to '{pattern}' in source code");
            }

            if (Regex.IsMatch(code, @"\breturn\b") == false)
            {
                var trimmed = code.TrimEnd().TrimEnd(';');
                code = $"return (object)({trimmed});";
            }

            var source = BuildSource(code, extraUsings);
            return CompileAndExecute(source);
        }

        private static string BuildSource(string code, string[] extraUsings)
        {
            var sb = new StringBuilder();
            foreach (var u in DefaultUsings)
                sb.AppendLine($"using {u};");
            if (extraUsings != null)
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
            var provider = new CSharpCodeProvider();
            var cp = new CompilerParameters
            {
                GenerateInMemory = true,
                GenerateExecutable = false,
                TreatWarningsAsErrors = false
            };

            var references = new List<string>();
            var added = new HashSet<string>();
            foreach (var asm in AppDomain.CurrentDomain.GetAssemblies())
            {
                try
                {
                    if (asm.IsDynamic || string.IsNullOrEmpty(asm.Location)) continue;
                    var name = asm.GetName().Name;
                    if (!added.Add(name)) continue;
                    if (name == "mscorlib") continue;
                    if (IsBclFacade(asm)) continue;
                    if (IsBlockedAssembly(name)) continue;
                    references.Add(asm.Location);
                }
                catch { }
            }

            // Use a response file to avoid Windows command line length limits (32,767 chars).
            // Large Unity projects can load 300+ assemblies, easily exceeding this limit.
            string rspPath = null;
            try
            {
                rspPath = System.IO.Path.Combine(System.IO.Path.GetTempPath(), $"unity_cli_{Guid.NewGuid():N}.rsp");
                var rspContent = new StringBuilder();
                foreach (var r in references)
                    rspContent.AppendLine($"/r:\"{r}\"");
                System.IO.File.WriteAllText(rspPath, rspContent.ToString());
                cp.CompilerOptions = $"@\"{rspPath}\"";
            }
            catch
            {
                // Fallback: add references directly (may fail on large projects)
                foreach (var r in references)
                    cp.ReferencedAssemblies.Add(r);
            }

            try
            {
                var result = provider.CompileAssemblyFromSource(cp, source);
                if (result.Errors.HasErrors)
                {
                    var errors = new List<string>();
                    foreach (CompilerError err in result.Errors)
                        if (!err.IsWarning) errors.Add($"L{err.Line}: {err.ErrorText}");
                    return new ErrorResponse($"Compile error:\n{string.Join("\n", errors)}");
                }

                var method = result.CompiledAssembly.GetType("__CliDynamic")?.GetMethod("Execute");
                if (method == null)
                    return new ErrorResponse("Internal error: compiled type or method not found.");
                var output = method.Invoke(null, null);
                return new SuccessResponse("OK", Serialize(output, 0));
            }
            finally
            {
                if (rspPath != null) try { System.IO.File.Delete(rspPath); } catch { }
            }
        }

        private static bool IsBclFacade(Assembly asm)
        {
            var name = asm.GetName().Name;
            if (!name.StartsWith("System.")) return false;
            if (name.StartsWith("System.Private.")) return false;
            try
            {
                foreach (var attr in asm.GetCustomAttributesData())
                    if (attr.AttributeType.Name == "TypeForwardedToAttribute") return true;
            }
            catch { }
            return false;
        }

        private static bool IsBlockedAssembly(string name)
        {
            // Block assemblies that provide dangerous capabilities
            if (name == "System.Diagnostics.Process") return true;
            if (name.StartsWith("System.Net")) return true;
            if (name == "System.Runtime.InteropServices") return true;
            if (name.StartsWith("Microsoft.Win32")) return true;
            return false;
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
