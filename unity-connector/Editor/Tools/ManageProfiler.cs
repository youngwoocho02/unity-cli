using System.Collections.Generic;
using Newtonsoft.Json.Linq;
using UnityEditor.Profiling;
using UnityEditorInternal;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Read Unity Profiler data and control profiler state. Actions: read_frames, enable, disable, status, clear.")]
    public static class ManageProfiler
    {
        public static object HandleCommand(JObject parameters)
        {
            var action = parameters["action"]?.Value<string>()?.ToLowerInvariant();
            if (string.IsNullOrEmpty(action))
                return new ErrorResponse("'action' required. Valid: read_frames, enable, disable, status, clear.");

            switch (action)
            {
                case "read_frames": return ReadFrames(parameters);
                case "enable":
                    UnityEngine.Profiling.Profiler.enabled = true;
                    ProfilerDriver.enabled = true;
                    return new SuccessResponse("Profiler enabled.");
                case "disable":
                    ProfilerDriver.enabled = false;
                    UnityEngine.Profiling.Profiler.enabled = false;
                    return new SuccessResponse("Profiler disabled.");
                case "status":
                    int first = ProfilerDriver.firstFrameIndex, last = ProfilerDriver.lastFrameIndex;
                    return new SuccessResponse("Profiler status", new
                    {
                        enabled = ProfilerDriver.enabled,
                        firstFrame = first, lastFrame = last,
                        frameCount = last >= first ? last - first + 1 : 0,
                        isPlaying = Application.isPlaying
                    });
                case "clear":
                    ProfilerDriver.ClearAllFrames();
                    return new SuccessResponse("All profiler frames cleared.");
                default:
                    return new ErrorResponse($"Unknown action: '{action}'.");
            }
        }

        private static object ReadFrames(JObject parameters)
        {
            if (!ProfilerDriver.enabled)
                return new ErrorResponse("Profiler not enabled. Use action='enable' first.");

            int lastFrame = ProfilerDriver.lastFrameIndex, firstFrame = ProfilerDriver.firstFrameIndex;
            if (lastFrame < 0 || firstFrame < 0)
                return new ErrorResponse("No profiler frames available.");

            int frameCount = parameters["frameCount"]?.Value<int>() ?? 1;
            int threadIndex = parameters["thread"]?.Value<int>() ?? 0;
            string filter = parameters["filter"]?.Value<string>() ?? "";
            float minMs = parameters["minMs"]?.Value<float>() ?? 0.01f;

            frameCount = System.Math.Min(frameCount, lastFrame - firstFrame + 1);
            var frames = new List<object>();

            for (int f = 0; f < frameCount; f++)
            {
                int frameIndex = lastFrame - f;
                using var frameData = ProfilerDriver.GetRawFrameDataView(frameIndex, threadIndex);
                if (frameData == null || !frameData.valid) continue;

                var collected = new List<(string name, float ms, int children)>();
                for (int i = 0; i < frameData.sampleCount; i++)
                {
                    float timeMs = frameData.GetSampleTimeMs(i);
                    if (timeMs < minMs) continue;
                    string name = frameData.GetSampleName(i);
                    if (!string.IsNullOrEmpty(filter) && name.IndexOf(filter, System.StringComparison.OrdinalIgnoreCase) < 0) continue;
                    collected.Add((name, timeMs, frameData.GetSampleChildrenCount(i)));
                }
                collected.Sort((a, b) => b.ms.CompareTo(a.ms));

                var samples = new List<object>();
                foreach (var s in collected)
                    samples.Add(new { s.name, s.ms, s.children });

                frames.Add(new { frameIndex, threadIndex, sampleCount = frameData.sampleCount, samples });
            }

            return new SuccessResponse($"{frames.Count} frames read", new { frames });
        }
    }
}
