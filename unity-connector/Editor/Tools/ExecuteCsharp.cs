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
    [UnityCliTool(Description = "Execute arbitrary C# code at runtime. Full access to Unity and all loaded assemblies.")]
    public static class ExecuteCsharp
    {
        private static readonly string[] DefaultUsings =
        {
            "System",
            "System.Collections.Generic",
            "System.Linq",
            "System.Reflection",
            "UnityEngine",
            "UnityEditor",
        };

        public static object HandleCommand(JObject parameters)
        {
            var code = parameters["code"]?.Value<string>();
            if (string.IsNullOrEmpty(code))
                return new ErrorResponse("'code' required");

            var extraUsings = parameters["usings"]?.ToObject<string[]>();

            if (Regex.IsMatch(code, @"\breturn[\s;]") == false)
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
                    cp.ReferencedAssemblies.Add(asm.Location);
                }
                catch { }
            }

            var result = provider.CompileAssemblyFromSource(cp, source);
            if (result.Errors.HasErrors)
            {
                var errors = new List<string>();
                foreach (CompilerError err in result.Errors)
                    if (!err.IsWarning) errors.Add($"L{err.Line}: {err.ErrorText}");
                return new ErrorResponse($"Compile error:\n{string.Join("\n", errors)}");
            }

            var method = result.CompiledAssembly.GetType("__CliDynamic").GetMethod("Execute");
            var output = method.Invoke(null, null);
            return new SuccessResponse("OK", Serialize(output, 0));
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
