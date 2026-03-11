using System;
using Newtonsoft.Json.Linq;

namespace UnityCliConnector
{
    public class ToolParams
    {
        private readonly JObject _params;

        public ToolParams(JObject @params)
        {
            _params = @params ?? throw new ArgumentNullException(nameof(@params));
        }

        public Result<string> GetRequired(string key, string errorMessage = null)
        {
            var value = GetString(key);
            if (string.IsNullOrEmpty(value))
                return Result<string>.Error(errorMessage ?? $"'{key}' parameter is required.");
            return Result<string>.Success(value);
        }

        public string Get(string key, string defaultValue = null)
        {
            return GetString(key) ?? defaultValue;
        }

        public int? GetInt(string key, int? defaultValue = null)
        {
            var str = GetString(key);
            if (string.IsNullOrEmpty(str)) return defaultValue;
            return int.TryParse(str, out var result) ? result : defaultValue;
        }

        public bool GetBool(string key, bool defaultValue = false)
        {
            return ParamCoercion.CoerceBool(GetToken(key), defaultValue);
        }

        public JToken GetRaw(string key)
        {
            return GetToken(key);
        }

        private JToken GetToken(string key)
        {
            var token = _params[key];
            if (token != null) return token;

            var snakeKey = StringCaseUtility.ToSnakeCase(key);
            if (snakeKey != key)
            {
                token = _params[snakeKey];
                if (token != null) return token;
            }

            var camelKey = StringCaseUtility.ToCamelCase(key);
            if (camelKey != key)
                token = _params[camelKey];

            return token;
        }

        private string GetString(string key)
        {
            return GetToken(key)?.ToString();
        }
    }

    public class Result<T>
    {
        public bool IsSuccess { get; }
        public T Value { get; }
        public string ErrorMessage { get; }

        private Result(bool isSuccess, T value, string errorMessage)
        {
            IsSuccess = isSuccess;
            Value = value;
            ErrorMessage = errorMessage;
        }

        public static Result<T> Success(T value) => new Result<T>(true, value, null);
        public static Result<T> Error(string errorMessage) => new Result<T>(false, default, errorMessage);
    }
}
