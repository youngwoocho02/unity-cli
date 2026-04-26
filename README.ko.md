# unity-cli

[English](README.md) | [한국어](README.ko.md)

> 커맨드라인으로 Unity Editor를 제어합니다. AI 에이전트를 위해 만들었지만, 무엇이든 사용 가능합니다.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**서버 실행 없음. 설정 파일 없음. 프로세스 관리 없음. 명령어만 치면 됩니다.**

## 왜 만들었나

터미널에서 Unity를 제어하고 싶었습니다. 기존 MCP 기반 연동은 Python 런타임, WebSocket 릴레이, JSON-RPC 프로토콜 레이어, 설정 파일, 켜고 꺼야 하는 서버 프로세스, 도구 등록 절차, 수만 줄의 과잉 설계된 코드를 요구했습니다. 단순한 명령 하나 보내는 데 이 모든 게 필요했습니다.

게다가 AI 에이전트마다 MCP 설정과 연동을 따로 해줘야 했습니다. CLI는 그런 게 없습니다 — 셸 명령어를 실행할 수 있는 에이전트라면 바로 쓸 수 있습니다.

이상하다고 느꼈습니다. `curl`로 URL 하나 쏠 수 있는데, 왜 그 모든 게 필요한가?

그래서 정반대로 만들었습니다. Unity에 HTTP로 직접 통신하는 바이너리 하나. 서버를 띄울 필요 없이 — Unity 패키지가 자동으로 수신합니다. 설정 파일 없이 — Unity 인스턴스를 알아서 찾습니다. 도구 등록 없이 — 이름으로 바로 호출합니다. 캐싱도, 프로토콜 레이어도, 절차도 없습니다.

CLI 전체가 Go ~800줄(+ help text ~300줄), Unity 커넥터가 C# ~2,300줄입니다. 셸에서 Unity를 다루게 해주는 아주 얇은 레이어 — 그 본분에 충실합니다. 바이너리 설치하고, Unity 패키지 추가하면 끝입니다.

## 설치

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/youngwoocho02/unity-cli/master/install.sh | sh
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/youngwoocho02/unity-cli/master/install.ps1 | iex
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

특정 버전을 고정하려면 URL 끝에 태그를 추가하세요 (예: `#v0.2.21`).

추가 후 Unity를 열면 커넥터가 자동으로 시작됩니다. 별도 설정 불필요.

### 권장: Editor 쓰로틀링 비활성화

기본적으로 Unity는 창이 포커스를 잃으면 에디터 업데이트를 쓰로틀링합니다. 이 경우 Unity를 다시 클릭하기 전까지 CLI 명령이 실행되지 않을 수 있습니다.

**Edit → Preferences → General → Interaction Mode**에서 **No Throttling**으로 설정하세요.

이렇게 하면 Unity가 백그라운드에 있어도 CLI 명령이 즉시 처리됩니다.

## 빠른 시작

```bash
# Unity 연결 확인
unity-cli status

# 플레이 모드 진입 후 대기
unity-cli editor play --wait

# Unity 안에서 C# 코드 실행
unity-cli exec "return Application.dataPath;"

# 콘솔 로그 읽기
unity-cli console --type error,warning,log
```

## 동작 방식

```
터미널                                Unity Editor
──────                                ────────────
$ unity-cli editor play --wait
    │
    ├─ ~/.unity-cli/instances/*.json 스캔
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
2. `~/.unity-cli/instances/`에 프로젝트별 instance 파일을 기록하여 CLI가 연결할 수 있게 하고
3. 0.5초마다 instance 파일에 현재 상태를 갱신하고 (heartbeat)
4. 매 요청마다 리플렉션으로 `[UnityCliTool]` 클래스를 탐지하고
5. 수신된 명령을 메인 스레드의 해당 핸들러로 라우팅하고
6. 도메인 리로드(스크립트 재컴파일)에서도 유지됩니다

컴파일이나 리로드 직전에 상태(`compiling`, `reloading`)를 instance 파일에 기록합니다. 메인 스레드가 멈추면 timestamp 갱신이 중단되고, CLI는 새로운 timestamp가 찍힐 때까지 대기한 후 명령을 전송합니다.

## 내장 명령어

| 명령어 | 설명 |
|--------|------|
| `editor` | Unity Editor play/stop/pause/refresh 제어 |
| `console` | 콘솔 로그 읽기, 필터링, 지우기 |
| `exec` | Unity 안에서 임의 C# 코드 실행 |
| `test` | EditMode/PlayMode 테스트 실행 |
| `menu` | Unity 메뉴 아이템을 경로로 실행 |
| `reserialize` | Unity 시리얼라이저를 통해 에셋 재직렬화 |
| `screenshot` | Scene/Game 뷰를 PNG로 캡처 |
| `profiler` | 프로파일러 하이어라키 읽기, 녹화 제어 |
| `list` | 사용 가능한 모든 도구와 파라미터 스키마 표시 |
| `status` | Unity Editor 연결 상태 확인 |
| `update` | CLI 바이너리 자동 업데이트 |

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

# 에셋 새로고침 (플레이모드에서는 --force 없으면 차단)
unity-cli editor refresh

# 새로고침 + 스크립트 컴파일 (플레이모드에서는 --force 없으면 차단)
unity-cli editor refresh --compile

# 플레이모드 중 강제 새로고침
unity-cli editor refresh --force
```

### 콘솔 로그

```bash
# 에러 및 경고 로그 읽기 (기본값)
unity-cli console

# 모든 타입의 최근 20개 로그 읽기
unity-cli console --lines 20 --filter error,warning,log

# 에러만 읽기
unity-cli console --type error

# 스택 트레이스 포함 (user: 유저 코드만, full: 원본 그대로)
unity-cli console --stacktrace user

# 콘솔 지우기
unity-cli console --clear
```

### C# 코드 실행

가장 강력한 명령어입니다. Unity Editor 런타임에서 임의의 C# 코드를 실행합니다. UnityEngine, UnityEditor, ECS 및 로드된 모든 어셈블리에 접근 가능합니다. 일회성 조회나 수정을 위해 커스텀 도구를 만들 필요가 없습니다.

`return`으로 결과를 받습니다. 주요 namespace는 기본 포함. 프로젝트 전용 타입(예: `Unity.Entities`)만 `--usings`로 추가합니다.

```bash
unity-cli exec "return Application.dataPath;"
unity-cli exec "return EditorSceneManager.GetActiveScene().name;"
unity-cli exec "return World.All.Count;" --usings Unity.Entities

# stdin으로 파이프하면 shell escaping 문제 없음
echo 'Debug.Log("hello"); return null;' | unity-cli exec
echo 'var go = new GameObject("Marker"); go.tag = "EditorOnly"; return go.name;' | unity-cli exec
```

`exec`는 실제 C#을 컴파일하고 실행하므로, 커스텀 도구가 할 수 있는 모든 것을 할 수 있습니다 — ECS 엔티티 조사, 에셋 수정, 내부 API 호출, 에디터 유틸리티 실행. AI 에이전트에게 이것은 **도구 코드를 한 줄도 작성하지 않고 Unity 전체 런타임에 즉시 접근**할 수 있다는 의미입니다. stdin 파이프를 사용하면 복잡한 코드에서 shell escaping 문제를 피할 수 있습니다.

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
# 전체 프로젝트 리시리얼라이즈 (인자 없이)
unity-cli reserialize

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
# 프로파일러 하이어라키 읽기 (마지막 프레임, 최상위)
unity-cli profiler hierarchy

# 재귀적 드릴다운
unity-cli profiler hierarchy --depth 3

# 이름으로 root 지정 (substring match) — 특정 시스템에 집중
unity-cli profiler hierarchy --root SimulationSystem --depth 3

# 특정 item ID로 드릴다운
unity-cli profiler hierarchy --parent 4 --depth 2

# 최근 30프레임 평균
unity-cli profiler hierarchy --frames 30 --min 0.5

# 특정 프레임 범위 평균
unity-cli profiler hierarchy --from 100 --to 200

# 필터 및 정렬
unity-cli profiler hierarchy --min 0.5 --sort self --max 10

# 프로파일러 녹화 켜기/끄기
unity-cli profiler enable
unity-cli profiler disable

# 프로파일러 상태 확인
unity-cli profiler status

# 캡쳐된 프레임 초기화
unity-cli profiler clear
```

### 테스트 실행

Unity Test Framework를 통해 EditMode/PlayMode 테스트를 실행합니다.

```bash
# EditMode 테스트 실행 (기본값)
unity-cli test

# PlayMode 테스트 실행
unity-cli test --mode PlayMode

# 테스트 이름으로 필터링 (부분 문자열 매치)
unity-cli test --filter MyTestClass
```

Unity Test Framework 패키지가 필요합니다. PlayMode 테스트는 도메인 리로드를 트리거하며, CLI가 자동으로 결과를 폴링합니다.

### 도구 목록

```bash
# 사용 가능한 모든 도구 (내장 + 프로젝트 커스텀)와 파라미터 스키마 표시
unity-cli list
```

### 커스텀 도구

```bash
# 커스텀 도구를 이름으로 직접 호출
unity-cli my_custom_tool

# 파라미터와 함께 호출
unity-cli my_custom_tool --params '{"key": "value"}'
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
| `--port <N>` | 활성 heartbeat 포트로 Unity 인스턴스 선택 | auto |
| `--project <path>` | 프로젝트 경로로 Unity 인스턴스 선택 | latest |
| `--timeout <ms>` | HTTP 요청 타임아웃 | 120000 |

```bash
# 특정 Unity 인스턴스에 연결
unity-cli --port 8091 editor play

# 여러 Unity 인스턴스 중 프로젝트 경로로 선택
unity-cli --project MyGame editor stop
```

모든 명령어에 `--help`를 붙이면 상세 사용법을 볼 수 있습니다:

```bash
unity-cli editor --help
unity-cli exec --help
unity-cli profiler --help
```

## 커스텀 도구 만들기

Editor 어셈블리에 `[UnityCliTool]` 어트리뷰트를 가진 static 클래스를 만드세요. 도메인 리로드 시 자동으로 탐지됩니다.

```csharp
using UnityCliConnector;
using Newtonsoft.Json.Linq;
using UnityEngine;

[UnityCliTool(Name = "spawn", Description = "지정 위치에 적 스폰", Group = "gameplay")]
public static class SpawnEnemy
{
    public class Parameters
    {
        [ToolParameter("X 월드 좌표", Required = true)]
        public float X { get; set; }

        [ToolParameter("Y 월드 좌표", Required = true)]
        public float Y { get; set; }

        [ToolParameter("Z 월드 좌표", Required = true)]
        public float Z { get; set; }

        [ToolParameter("Resources 폴더 내 프리팹 이름", DefaultValue = "Enemy")]
        public string Prefab { get; set; }
    }

    public static object HandleCommand(JObject parameters)
    {
        var p = new ToolParams(parameters);
        float x = p.GetFloat("x", 0);
        float y = p.GetFloat("y", 0);
        float z = p.GetFloat("z", 0);
        string prefabName = p.Get("prefab", "Enemy");

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

flag 또는 JSON으로 호출:

```bash
unity-cli spawn --x 1 --y 0 --z 5 --prefab Goblin
unity-cli spawn --params '{"x":1,"y":0,"z":5,"prefab":"Goblin"}'
```

**주요 사항:**

- **이름**: `Name` 없으면 클래스명에서 자동 생성 (`SpawnEnemy` → `spawn_enemy`, `UITree` → `ui_tree`). `Name = "spawn"`이면 `unity-cli spawn`으로 호출.
- **Parameters 클래스**: 선택 사항이지만 권장. `unity-cli list`에서 파라미터 이름, 타입, 설명, 필수 여부를 노출 — AI 어시스턴트가 소스 코드 없이 도구 사용법을 파악 가능.
- **ToolParams**: `p.Get()`, `p.GetInt()`, `p.GetFloat()`, `p.GetBool()`, `p.GetRaw()`로 일관된 파라미터 읽기.
- **탐지**: `unity-cli list`에서 built-in 도구(`group: "built-in"`)가 먼저, 연결된 프로젝트의 custom 도구(`group: "custom"`)가 뒤에 표시.

**어트리뷰트 참조:**

| 어트리뷰트 | 속성 | 설명 |
|---|---|---|
| `[UnityCliTool]` | `Name` | 명령어 이름 오버라이드 (기본: 클래스명 → snake_case) |
| | `Description` | `list`에 표시되는 도구 설명 |
| | `Group` | 분류용 그룹 이름 |
| `[ToolParameter]` | `Description` | 파라미터 설명 (생성자 인자) |
| | `Required` | 필수 여부 (기본: `false`) |
| | `Name` | 파라미터 이름 오버라이드 |
| | `DefaultValue` | 기본값 힌트 |

### 규칙

- 클래스는 `static`이어야 합니다
- `public static object HandleCommand(JObject parameters)` 또는 `async Task<object>` 변형이 필요합니다
- `SuccessResponse(message, data)` 또는 `ErrorResponse(message)`를 반환하세요
- `Parameters` 중첩 클래스에 `[ToolParameter]` 어트리뷰트를 추가하면 자동 문서화됩니다
- 클래스 이름이 자동으로 snake_case 명령어 이름으로 변환됩니다
- `[UnityCliTool(Name = "my_name")]`으로 이름을 재정의할 수 있습니다
- Unity 메인 스레드에서 실행되므로 모든 Unity API를 안전하게 호출할 수 있습니다
- Editor 시작 시와 스크립트 재컴파일 후 자동으로 탐지됩니다
- 중복된 도구 이름은 감지되어 에러로 로그됩니다 — 먼저 발견된 핸들러만 사용됩니다

## 여러 Unity 인스턴스

여러 Unity Editor가 열려 있으면, 각각 다른 포트(8090, 8091, ...)에 등록됩니다:

```bash
# 실행 중인 모든 인스턴스 확인
ls ~/.unity-cli/instances/

# 프로젝트 경로로 선택
unity-cli --project MyGame editor play

# 활성 heartbeat 포트로 선택
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
