using Newtonsoft.Json.Linq;

namespace UnityCliConnector
{
    public static class ParamCoercion
    {
        public static bool CoerceBool(JToken token, bool defaultValue)
        {
            return CoerceBoolNullable(token) ?? defaultValue;
        }

        public static bool? CoerceBoolNullable(JToken token)
        {
            if (token == null || token.Type == JTokenType.Null)
                return null;
            try
            {
                if (token.Type == JTokenType.Boolean)
                    return token.Value<bool>();
                var s = token.ToString().Trim().ToLowerInvariant();
                if (s.Length == 0) return null;
                if (bool.TryParse(s, out var b)) return b;
                if (s == "1" || s == "yes" || s == "on") return true;
                if (s == "0" || s == "no" || s == "off") return false;
            }
            catch { }
            return null;
        }
    }
}
