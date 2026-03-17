using System;
using System.Collections.Generic;
using System.Linq;
using Newtonsoft.Json.Linq;
using UnityEditor.Profiling;
using UnityEditorInternal;
using UnityEngine;

namespace UnityCliConnector.Tools
{
    [UnityCliTool(Description = "Control Unity Profiler. Actions: hierarchy, enable, disable, status, clear.")]
    public static class ManageProfiler
    {
        public class Parameters
        {
            [ToolParameter("Action: hierarchy, enable, disable, status, or clear", Required = true)]
            public string Action { get; set; }

            [ToolParameter("Frame index. -1 or omit = last captured frame.")]
            public int Frame { get; set; }

            [ToolParameter("Start frame index for range average.")]
            public int From { get; set; }

            [ToolParameter("End frame index for range average.")]
            public int To { get; set; }

            [ToolParameter("Number of recent frames to average (shortcut for range).")]
            public int Frames { get; set; }

            [ToolParameter("Thread index. 0 = main thread.")]
            public int ThreadIndex { get; set; }

            [ToolParameter("Parent item ID to drill into. Omit for root level.")]
            public int ParentId { get; set; }

            [ToolParameter("Find item by name and use as root. Substring match.")]
            public string Root { get; set; }

            [ToolParameter("Minimum total time (ms) filter.")]
            public float MinTime { get; set; }

            [ToolParameter("Sort column: 'total', 'self', or 'calls'. Default 'total'.")]
            public string SortBy { get; set; }

            [ToolParameter("Max children per level. Default 30.")]
            public int MaxItems { get; set; }

            [ToolParameter("Recursive depth. 1 = one level (default), 0 = unlimited.")]
            public int Depth { get; set; }
        }

        public static object HandleCommand(JObject parameters)
        {
            var p = new ToolParams(parameters);
            var action = p.Get("action")?.ToLowerInvariant();
            if (string.IsNullOrEmpty(action))
                return new ErrorResponse("'action' required. Valid: hierarchy, enable, disable, status, clear.");

            switch (action)
            {
                case "hierarchy": return Hierarchy(p);
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
                    return new ErrorResponse($"Unknown action: '{action}'. Valid: hierarchy, enable, disable, status, clear.");
            }
        }

        private static object Hierarchy(ToolParams p)
        {
            if (ProfilerDriver.enabled == false && ProfilerDriver.lastFrameIndex < 0)
                return new ErrorResponse("Profiler has no captured data. Enable profiler first.");

            var fromFrame = p.GetInt("from", -1).Value;
            var toFrame = p.GetInt("to", -1).Value;
            var framesCount = p.GetInt("frames", 0).Value;

            if (fromFrame >= 0 || toFrame >= 0)
            {
                if (fromFrame < 0) fromFrame = ProfilerDriver.firstFrameIndex;
                if (toFrame < 0) toFrame = ProfilerDriver.lastFrameIndex;
                return AveragedHierarchy(p, fromFrame, toFrame);
            }
            if (framesCount > 1)
                return AveragedHierarchy(p, ProfilerDriver.lastFrameIndex - framesCount + 1, ProfilerDriver.lastFrameIndex);

            var frameIndex = p.GetInt("frame", -1).Value;
            if (frameIndex < 0) frameIndex = ProfilerDriver.lastFrameIndex;
            if (frameIndex < ProfilerDriver.firstFrameIndex || frameIndex > ProfilerDriver.lastFrameIndex)
                return new ErrorResponse(
                    $"Frame {frameIndex} out of range [{ProfilerDriver.firstFrameIndex}..{ProfilerDriver.lastFrameIndex}]");

            var threadIndex = p.GetInt("threadIndex", 0).Value;
            var parentIdToken = p.GetRaw("parentId");
            var rootName = p.Get("root");
            var minTime = p.GetFloat("minTime", 0f).Value;
            var sortBy = (p.Get("sortBy", "total")).ToLowerInvariant();
            var maxItems = p.GetInt("maxItems", 30).Value;
            if (maxItems <= 0) maxItems = 30;
            var depth = p.GetInt("depth", 1).Value;
            if (depth <= 0) depth = 999;

            int sortColumn = GetSortColumn(sortBy);

            using var frameData = ProfilerDriver.GetHierarchyFrameDataView(
                frameIndex, threadIndex,
                HierarchyFrameDataView.ViewModes.MergeSamplesWithTheSameName,
                sortColumn, false);

            if (frameData == null || frameData.valid == false)
                return new ErrorResponse($"No profiler data for frame {frameIndex}, thread {threadIndex}.");

            // Must traverse from root first — Unity lazy-initializes the hierarchy tree.
            int rootId = frameData.GetRootItemID();
            var rootChildIds = new List<int>();
            frameData.GetItemChildren(rootId, rootChildIds);

            int parentId = rootId;
            string parentName = "(root)";

            // --root: find by name
            if (!string.IsNullOrEmpty(rootName))
            {
                int found = FindItemByName(frameData, rootId, rootName);
                if (found < 0)
                    return new ErrorResponse($"No profiler item matching '{rootName}' found.");
                parentId = found;
                parentName = frameData.GetItemName(found);
            }
            // --parent: by ID
            else if (parentIdToken != null && parentIdToken.Type != JTokenType.Null)
            {
                parentId = parentIdToken.Value<int>();
                parentName = frameData.GetItemName(parentId);
            }

            var items = BuildChildren(frameData, parentId, minTime, maxItems, depth);

            var result = new JObject
            {
                ["frame"] = frameIndex,
                ["threadIndex"] = threadIndex,
                ["parentId"] = parentId,
                ["parentName"] = parentName,
                ["depth"] = depth >= 999 ? 0 : depth,
                ["children"] = items,
            };

            return new SuccessResponse($"Hierarchy of '{parentName}' (frame {frameIndex})", result);
        }

        private static object AveragedHierarchy(ToolParams p, int fromFrame, int toFrame)
        {
            int firstAvail = ProfilerDriver.firstFrameIndex;
            int lastAvail = ProfilerDriver.lastFrameIndex;
            fromFrame = Math.Max(fromFrame, firstAvail);
            toFrame = Math.Min(toFrame, lastAvail);
            int frameCount = toFrame - fromFrame + 1;
            if (frameCount <= 0)
                return new ErrorResponse($"No frames in range [{fromFrame}..{toFrame}]. Available: [{firstAvail}..{lastAvail}].");

            var threadIndex = p.GetInt("threadIndex", 0).Value;
            var rootName = p.Get("root");
            var minTime = p.GetFloat("minTime", 0f).Value;
            var sortBy = (p.Get("sortBy", "total")).ToLowerInvariant();
            var maxItems = p.GetInt("maxItems", 30).Value;
            if (maxItems <= 0) maxItems = 30;
            var depth = p.GetInt("depth", 1).Value;
            if (depth <= 0) depth = 999;

            int sortColumn = GetSortColumn(sortBy);

            // Collect data across frames
            var accumulated = new Dictionary<string, (double totalMs, double selfMs, long calls, int count)>();

            for (int frameIndex = fromFrame; frameIndex <= toFrame; frameIndex++)
            {
                using var frameData = ProfilerDriver.GetHierarchyFrameDataView(
                    frameIndex, threadIndex,
                    HierarchyFrameDataView.ViewModes.MergeSamplesWithTheSameName,
                    sortColumn, false);

                if (frameData == null || frameData.valid == false) continue;

                int rootId = frameData.GetRootItemID();
                var rootChildIds = new List<int>();
                frameData.GetItemChildren(rootId, rootChildIds);

                int parentId = rootId;
                if (!string.IsNullOrEmpty(rootName))
                {
                    int found = FindItemByName(frameData, rootId, rootName);
                    if (found >= 0) parentId = found;
                }

                CollectFlat(frameData, parentId, depth, accumulated);
            }

            // Build averaged result
            var sorted = accumulated
                .Select(kv => new
                {
                    name = kv.Key,
                    avgTotalMs = Math.Round(kv.Value.totalMs / kv.Value.count, 3),
                    avgSelfMs = Math.Round(kv.Value.selfMs / kv.Value.count, 3),
                    avgCalls = Math.Round((double)kv.Value.calls / kv.Value.count, 1),
                    appearedIn = kv.Value.count,
                })
                .Where(x => x.avgTotalMs >= minTime)
                .OrderByDescending(x => sortBy == "self" ? x.avgSelfMs : x.avgTotalMs)
                .Take(maxItems)
                .ToList();

            string rootLabel = string.IsNullOrEmpty(rootName) ? "(root)" : rootName;

            return new SuccessResponse($"Averaged over {frameCount} frames", new
            {
                frameCount,
                threadIndex,
                root = rootLabel,
                items = sorted,
            });
        }

        private static void CollectFlat(HierarchyFrameDataView frameData, int parentId, int remainingDepth,
            Dictionary<string, (double totalMs, double selfMs, long calls, int count)> acc)
        {
            var childIds = new List<int>();
            frameData.GetItemChildren(parentId, childIds);

            foreach (var childId in childIds)
            {
                var name = frameData.GetItemName(childId);
                var totalMs = frameData.GetItemColumnDataAsFloat(childId, HierarchyFrameDataView.columnTotalTime);
                var selfMs = frameData.GetItemColumnDataAsFloat(childId, HierarchyFrameDataView.columnSelfTime);
                var calls = (long)frameData.GetItemColumnDataAsFloat(childId, HierarchyFrameDataView.columnCalls);

                if (acc.TryGetValue(name, out var existing))
                    acc[name] = (existing.totalMs + totalMs, existing.selfMs + selfMs, existing.calls + calls, existing.count + 1);
                else
                    acc[name] = (totalMs, selfMs, calls, 1);

                if (remainingDepth > 1)
                    CollectFlat(frameData, childId, remainingDepth - 1, acc);
            }
        }

        private static int FindItemByName(HierarchyFrameDataView frameData, int parentId, string name)
        {
            var childIds = new List<int>();
            frameData.GetItemChildren(parentId, childIds);

            foreach (var childId in childIds)
            {
                var itemName = frameData.GetItemName(childId);
                if (itemName.IndexOf(name, StringComparison.OrdinalIgnoreCase) >= 0)
                    return childId;

                // Recurse to find nested items
                int found = FindItemByName(frameData, childId, name);
                if (found >= 0) return found;
            }

            return -1;
        }

        static int GetSortColumn(string sortBy)
        {
            switch (sortBy)
            {
                case "self": return HierarchyFrameDataView.columnSelfTime;
                case "calls": return HierarchyFrameDataView.columnCalls;
                default: return HierarchyFrameDataView.columnTotalTime;
            }
        }

        static JArray BuildChildren(HierarchyFrameDataView frameData, int parentId, float minTime, int maxItems, int remainingDepth)
        {
            var childIds = new List<int>();
            frameData.GetItemChildren(parentId, childIds);

            var items = new JArray();
            int shown = 0;
            foreach (var childId in childIds)
            {
                var totalTime = frameData.GetItemColumnDataAsFloat(childId, HierarchyFrameDataView.columnTotalTime);
                if (totalTime < minTime) continue;
                if (shown >= maxItems) break;
                shown++;

                var selfTime = frameData.GetItemColumnDataAsFloat(childId, HierarchyFrameDataView.columnSelfTime);
                var calls = (int)frameData.GetItemColumnDataAsFloat(childId, HierarchyFrameDataView.columnCalls);

                var item = new JObject
                {
                    ["itemId"] = childId,
                    ["name"] = frameData.GetItemName(childId),
                    ["totalMs"] = Math.Round(totalTime, 3),
                    ["selfMs"] = Math.Round(selfTime, 3),
                    ["calls"] = calls,
                };

                if (remainingDepth > 1)
                {
                    var subChildren = BuildChildren(frameData, childId, minTime, maxItems, remainingDepth - 1);
                    if (subChildren.Count > 0)
                        item["children"] = subChildren;
                }

                items.Add(item);
            }

            return items;
        }
    }
}
