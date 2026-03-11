# unity-cli

[English](README.md) | [한국어](README.ko.md)

> 커맨드라인으로 Unity Editor를 제어합니다. AI 에이전트를 위해 만들었지만, 무엇이든 사용 가능합니다.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**MCP 프로토콜 없음. Python 릴레이 없음. 런타임 의존성 없음. 바이너리 하나.**

## 왜 MCP가 아닌가?

MCP 기반 Unity 연동은 Python 릴레이, WebSocket 브릿지, JSON-RPC 프로토콜 레이어, 런타임 의존성 등 수만 줄의 코드로 이루어져 있습니다. 설치가 어렵고, 디버깅이 어렵고, 소스를 읽지 않으면 이해할 수 없는 시스템입니다.

이 프로젝트는 정반대의 접근을 택합니다. CLI 전체가 Go ~500줄, Unity 커넥터가 C# ~1,500줄입니다. 프로토콜 레이어도, 릴레이 프로세스도, 가상 환경도 없습니다 — 바이너리에서 Unity HttpListener로 직접 HTTP POST를 보내는 것이 전부입니다.

셸 명령어를 실행할 수 있다면, Unity를 제어할 수 있습니다.

## 설치

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/youngwoocho02/unity-cli/master/install.sh | sh
```

### Windows (PowerShell)

```powershell
Invoke-WebRequest -Uri "https://github.com/youngwoocho02/unity-cli/releases/latest/download/unity-cli-windows-amd64.exe" -OutFile "$env:LOCALAPPDATA\unity-cli.exe"; if($env:Path -notlike "*$env:LOCALAPPDATA*"){[Environment]::SetEnvironmentVariable("Path","$([Environment]::GetEnvironmentVariable('Path','User'));$env:LOCALAPPDATA","User");$env:Path+= ";$env:LOCALAPPDATA"}; unity-cli version
```

### 기타 방법

```bash
# Go install (Go가 설치된 모든 플랫폼)
go install github.com/youngwoocho02/unity-cli@latest

# 수동 다운로드 (플랫폼 선택)
# Linux amd64 / Linux arm64 / macOS amd64 / macOS arm64 / Windows amd64
curl -fsSL https://github.com/youngwoocho02/unity-cli/releases/latest/download/unity-cli-linux-amd64 -o unity-cli
chmod +x unity-cli && sudo mv unity-cli /usr/local/bin/
```

지원 플랫폼: Linux (amd64, arm64), macOS (Intel, Apple Silicon), Windows (amd64).

### 업데이트

```bash
# 최신 버전으로 자동 업데이트
unity-cli update

# 새 버전 확인만
unity-cli update --check
```

## Unity 설정

**Package Manager → Add package from git URL**에서 추가:

```
https://github.com/youngwoocho02/unity-cli.git?path=unity-connector
```

또는 `Packages/manifest.json`에 직접 추가:
```json
"com.youngwoocho02.unity-cli-connector": "https://github.com/youngwoocho02/unity-cli.git?path=unity-connector"
```

특정 버전을 고정하려면 URL 끝에 `#v0.1.0`을 추가하세요.

추가 후 Unity를 열면 커넥터가 자동으로 시작됩니다. 별도 설정 불필요.

### 권장: Editor 쓰로틀링 비활성화

기본적으로 Unity는 창이 포커스를 잃으면 에디터 업데이트를 쓰로틀링합니다. 이 경우 Unity를 다시 클릭하기 전까지 CLI 명령이 실행되지 않을 수 있습니다.

**Edit → Preferences → General → Interaction Mode**에서 **No Throttling**으로 설정하세요.

이렇게 하면 Unity가 백그라운드에 있어도 CLI 명령이 즉시 처리됩니다.

## 동작 방식

```
터미널                                Unity Editor
──────                                ────────────
$ unity-cli editor play --wait
    │
    ├─ ~/.unity-cli/instances.json 읽기
    │  → Unity가 포트 8090에 있음을 확인
    │
    ├─ POST http://127.0.0.1:8090/command
    │  { "command": "manage_editor",
    │    "params": { "action": "play",
    │                "wait_for_completion": true }}
    │                                      │
    │                                  HttpServer 수신
    │                                      │
    │                                  CommandRouter 디스패치
    │                                      │
    │                                  ManageEditor.HandleCommand()
    │                                  → EditorApplication.isPlaying = true
    │                                  → PlayModeStateChange 대기
    │                                      │
    ├─ JSON 응답 수신  ←──────────────────┘
    │  { "success": true,
    │    "message": "Entered play mode (confirmed)." }
    │
    └─ 출력: Entered play mode (confirmed).
```

Unity 커넥터의 동작:
1. Editor 시작 시 `localhost:8090`에 HTTP 서버를 열고
2. `~/.unity-cli/instances.json`에 자신을 등록하여 CLI가 연결할 수 있게 하고
3. `~/.unity-cli/status/{port}.json`에 0.5초마다 현재 상태를 기록하고
4. 매 요청마다 리플렉션으로 `[UnityCliTool]` 클래스를 탐지하고
5. 수신된 명령을 메인 스레드의 해당 핸들러로 라우팅하고
6. 도메인 리로드(스크립트 재컴파일)에서도 유지됩니다

컴파일이나 리로드 직전에 상태(`compiling`, `reloading`)를 status 파일에 기록합니다. 메인 스레드가 멈추면 timestamp 갱신이 중단되고, CLI는 새로운 timestamp가 찍힐 때까지 대기한 후 명령을 전송합니다.

## 내장 명령어

### Editor 제어

```bash
# 플레이 모드 진입
unity-cli editor play

# 플레이 모드 진입 후 완전히 로드될 때까지 대기
unity-cli editor play --wait

# 플레이 모드 종료
unity-cli editor stop

# 일시정지 토글 (플레이 모드에서만 동작)
unity-cli editor pause

# 에셋 새로고침
unity-cli editor refresh

# 새로고침 + 스크립트 컴파일 (컴파일 완료까지 대기)
unity-cli editor refresh --compile
```

### 콘솔 로그

```bash
# 에러 및 경고 로그 읽기 (기본값)
unity-cli console

# 모든 타입의 최근 20개 로그 읽기
unity-cli console --lines 20 --filter all

# 에러만 읽기
unity-cli console --filter error

# 콘솔 지우기
# (exec 사용)
unity-cli exec "typeof(UnityEditor.LogEntries).GetMethod(\"Clear\", System.Reflection.BindingFlags.Static | System.Reflection.BindingFlags.Public).Invoke(null, null); return \"cleared\";"
```

### C# 코드 실행

가장 강력한 명령어입니다. Unity Editor 런타임에서 임의의 C# 코드를 실행합니다. UnityEngine, UnityEditor, ECS 및 로드된 모든 어셈블리에 접근 가능합니다. 일회성 조회나 수정을 위해 커스텀 도구를 만들 필요가 없습니다.

단순 표현식은 결과를 자동 반환합니다. 여러 문장일 때는 명시적 `return`이 필요합니다.

```bash
# 단순 표현식
unity-cli exec "Time.time"
unity-cli exec "Application.dataPath"
unity-cli exec "EditorSceneManager.GetActiveScene().name"

# 게임 오브젝트 조회
unity-cli exec "GameObject.FindObjectsOfType<Camera>().Length"
unity-cli exec "Selection.activeGameObject?.name ?? \"nothing selected\""

# 여러 문장 (명시적 return)
unity-cli exec "var go = new GameObject(\"Marker\"); go.tag = \"EditorOnly\"; return go.name;"

# ECS 월드 조사 (추가 using 포함)
unity-cli exec "World.All.Count" --usings Unity.Entities
unity-cli exec "var sb = new System.Text.StringBuilder(); foreach(var w in World.All) sb.AppendLine(w.Name); return sb.ToString();" --usings Unity.Entities

# 런타임에서 프로젝트 설정 수정
unity-cli exec "PlayerSettings.bundleVersion = \"1.2.3\"; return PlayerSettings.bundleVersion;"

# ScriptableObject 접근
unity-cli exec "Resources.Load<GameSettings>(\"GameSettings\").maxPlayers" --usings YourNamespace
```

`exec`는 실제 C#을 컴파일하고 실행하므로, 커스텀 도구가 할 수 있는 모든 것을 할 수 있습니다 — ECS 엔티티 조사, 에셋 수정, 내부 API 호출, 에디터 유틸리티 실행. AI 에이전트에게 이것은 **도구 코드를 한 줄도 작성하지 않고 Unity 전체 런타임에 즉시 접근**할 수 있다는 의미입니다.

### 메뉴 아이템

```bash
# Unity 메뉴 아이템을 경로로 실행
unity-cli menu "File/Save Project"
unity-cli menu "Assets/Refresh"
unity-cli menu "Window/General/Console"
```

안전을 위해 `File/Quit`은 차단됩니다.

### 에셋 리시리얼라이즈

AI 에이전트(와 사람)는 Unity 에셋 파일 — `.prefab`, `.unity`, `.asset`, `.mat` — 을 텍스트 YAML로 직접 수정할 수 있습니다. 하지만 Unity의 YAML 시리얼라이저는 엄격합니다: 필드 누락, 잘못된 들여쓰기, 오래된 `fileID` 하나면 에셋이 조용히 깨집니다.

`reserialize`가 이걸 해결합니다. 텍스트 수정 후 실행하면 Unity가 에셋을 메모리에 로드한 뒤 자체 시리얼라이저로 다시 기록합니다. Inspector에서 수정한 것과 동일한, 깨끗하고 유효한 YAML 파일이 됩니다.

```bash
# 프리팹의 Transform 값을 텍스트로 수정한 후
unity-cli reserialize Assets/Prefabs/Player.prefab

# 여러 씬을 일괄 수정한 후
unity-cli reserialize Assets/Scenes/Main.unity Assets/Scenes/Lobby.unity

# 머티리얼 속성 수정 후
unity-cli reserialize Assets/Materials/Character.mat
```

이것이 텍스트 기반 에셋 수정을 안전하게 만드는 핵심입니다. 이게 없으면 YAML 필드 하나 잘못 놓은 것이 런타임에서야 드러나는 프리팹 파손으로 이어집니다. 이게 있으면 **AI 에이전트가 어떤 Unity 에셋이든 텍스트로 자신 있게 수정**할 수 있습니다 — 프리팹에 컴포넌트 추가, 씬 계층 구조 변경, 머티리얼 속성 조정 — 결과가 정상적으로 로드된다는 것을 보장하면서.

### 프로파일러

```bash
# 프로파일러 하이어라키 읽기 (마지막 프레임)
unity-cli profiler hierarchy

# 깊이 제한
unity-cli profiler hierarchy --depth 3
```

### 커스텀 도구

```bash
# 등록된 모든 도구 목록 (내장 + 프로젝트 커스텀)
unity-cli tool list

# 커스텀 도구 호출
unity-cli tool call my_custom_tool --params '{"key": "value"}'

# 도구 도움말
unity-cli tool help my_custom_tool
```

### 상태 확인

```bash
# Unity Editor 상태 확인
unity-cli status
# 출력: Unity (port 8090): ready
#   Project: /path/to/project
#   Version: 6000.1.0f1
#   PID:     12345
```

명령 전송 전에 CLI가 자동으로 Unity 상태를 확인합니다. Unity가 바쁜 상태(컴파일, 리로드)이면 응답 가능해질 때까지 대기합니다.

## 글로벌 옵션

| 플래그 | 설명 | 기본값 |
|--------|------|--------|
| `--port <N>` | Unity 인스턴스 포트 직접 지정 (자동 탐지 건너뜀) | auto |
| `--project <path>` | 프로젝트 경로로 Unity 인스턴스 선택 | latest |
| `--json` | 원시 JSON 응답 출력 | off |
| `--timeout <ms>` | HTTP 요청 타임아웃 | 120000 |

```bash
# 특정 Unity 인스턴스에 연결
unity-cli --port 8091 editor play

# 여러 Unity 인스턴스 중 프로젝트 경로로 선택
unity-cli --project MyGame editor stop

# 원시 JSON 출력 (AI 파싱에 유용)
unity-cli --json console --lines 10
```

## 커스텀 도구 만들기

Editor 어셈블리에 `[UnityCliTool]` 어트리뷰트를 가진 static 클래스를 만드세요. 도메인 리로드 시 자동으로 탐지됩니다.

```csharp
using UnityCliConnector;
using Newtonsoft.Json.Linq;

[UnityCliTool(Description = "지정 위치에 적 스폰")]
public static class SpawnEnemy
{
    // 명령어 이름 자동 생성: "spawn_enemy"
    // 호출: unity-cli tool call spawn_enemy --params '{"x":1,"y":0,"z":5}'

    public class Parameters
    {
        [ToolParameter("X 월드 좌표", Required = true)]
        public float X { get; set; }

        [ToolParameter("Y 월드 좌표", Required = true)]
        public float Y { get; set; }

        [ToolParameter("Z 월드 좌표", Required = true)]
        public float Z { get; set; }

        [ToolParameter("Resources 폴더 내 프리팹 이름")]
        public string Prefab { get; set; }
    }

    public static object HandleCommand(JObject parameters)
    {
        float x = parameters["x"]?.Value<float>() ?? 0;
        float y = parameters["y"]?.Value<float>() ?? 0;
        float z = parameters["z"]?.Value<float>() ?? 0;
        string prefabName = parameters["prefab"]?.Value<string>() ?? "Enemy";

        var prefab = Resources.Load<GameObject>(prefabName);
        var instance = Object.Instantiate(prefab, new Vector3(x, y, z), Quaternion.identity);

        return new SuccessResponse("Enemy spawned", new
        {
            name = instance.name,
            position = new { x, y, z }
        });
    }
}
```

`Parameters` 클래스는 선택 사항이지만 권장됩니다. 있으면 `tool list`와 `tool help`에서 파라미터 이름, 타입, 설명, 필수 여부를 노출합니다 — AI 어시스턴트가 소스 코드를 읽지 않고도 도구 사용법을 알 수 있습니다.

```bash
$ unity-cli tool help spawn_enemy
spawn_enemy — 지정 위치에 적 스폰
  x        float    (필수) X 월드 좌표
  y        float    (필수) Y 월드 좌표
  z        float    (필수) Z 월드 좌표
  prefab   string          Resources 폴더 내 프리팹 이름
```

### 규칙

- 클래스는 `static`이어야 합니다
- `public static object HandleCommand(JObject parameters)` 또는 `async Task<object>` 변형이 필요합니다
- `SuccessResponse(message, data)` 또는 `ErrorResponse(message)`를 반환하세요
- `Parameters` 중첩 클래스에 `[ToolParameter]` 어트리뷰트를 추가하면 자동 문서화됩니다
- 클래스 이름이 자동으로 snake_case 명령어 이름으로 변환됩니다
- `[UnityCliTool(Name = "my_name")]`으로 이름을 재정의할 수 있습니다
- Unity 메인 스레드에서 실행되므로 모든 Unity API를 안전하게 호출할 수 있습니다
- Editor 시작 시와 스크립트 재컴파일 후 자동으로 탐지됩니다

## 여러 Unity 인스턴스

여러 Unity Editor가 열려 있으면, 각각 다른 포트(8090, 8091, ...)에 등록됩니다:

```bash
# 실행 중인 모든 인스턴스 확인
cat ~/.unity-cli/instances.json

# 프로젝트 경로로 선택
unity-cli --project MyGame editor play

# 포트로 선택
unity-cli --port 8091 editor play

# 기본: 가장 최근 등록된 인스턴스 사용
unity-cli editor play
```

## MCP와 비교

| | MCP | unity-cli |
|---|-----|-----------|
| **설치** | Python + uv + FastMCP + config JSON | 바이너리 하나 |
| **의존성** | Python 런타임, WebSocket 릴레이 | 없음 |
| **프로토콜** | JSON-RPC 2.0 over stdio + WebSocket | 직접 HTTP POST |
| **설정** | MCP 설정 생성, AI 도구 재시작 | Unity 패키지 추가, 끝 |
| **재연결** | 복잡한 도메인 리로드 재연결 로직 | 요청별 무상태 |
| **호환성** | MCP 호환 클라이언트만 | 셸이 있는 모든 것 |
| **커스텀 도구** | 동일한 `[Attribute]` + `HandleCommand` 패턴 | 동일 |

## 만든 사람

**DevBookOfArray**

[![YouTube](https://img.shields.io/badge/YouTube-DevBookOfArray-red?logo=youtube&logoColor=white)](https://www.youtube.com/@DevBookOfArray)
[![GitHub](https://img.shields.io/badge/GitHub-youngwoocho02-181717?logo=github)](https://github.com/youngwoocho02)

## 라이선스

MIT
