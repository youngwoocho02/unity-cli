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
            return Regex.Replace(str, "([a-z0-9])([A-Z])", "$1_$2").ToLowerInvariant();
        }

        public static string ToCamelCase(string str)
        {
            if (string.IsNullOrEmpty(str) || !str.Contains("_"))
                return str;

            var parts = str.Split('_');
            if (parts.Length == 0)
                return str;

            var first = parts[0];
            var rest = string.Concat(parts.Skip(1).Select(part =>
                string.IsNullOrEmpty(part) ? "" : char.ToUpperInvariant(part[0]) + part.Substring(1)));

            return first + rest;
        }
    }
}
