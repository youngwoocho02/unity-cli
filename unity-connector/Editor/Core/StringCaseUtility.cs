using System.Linq;
using System.Text.RegularExpressions;

namespace UnityCliConnector
{
    public static class StringCaseUtility
    {
        public static string ToSnakeCase(string str)
        {
            if (string.IsNullOrEmpty(str))
                return str;
            // Handle transitions: lower→Upper, digit→Upper, and UPPER→Upperlower (acronyms)
            var result = Regex.Replace(str, "([a-z0-9])([A-Z])", "$1_$2");
            result = Regex.Replace(result, "([A-Z]+)([A-Z][a-z])", "$1_$2");
            return result.ToLowerInvariant();
        }

        public static string ToCamelCase(string str)
        {
            if (string.IsNullOrEmpty(str) || !str.Contains("_"))
                return str;

            var parts = str.Split('_');
            var first = parts[0];
            var rest = string.Concat(parts.Skip(1).Select(part =>
                part.Length == 0 ? "" : char.ToUpperInvariant(part[0]) + (part.Length > 1 ? part.Substring(1) : "")));

            return first + rest;
        }
    }
}
