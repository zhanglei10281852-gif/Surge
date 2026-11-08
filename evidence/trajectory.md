# 按有序字节窗口下载可恢复的紧凑文件

> **Workspace:** F:\swe\099926\project-01

---

**User**

从远端大文件中取回几段已知位置的内容时，命令行使用者应当能直接下载这些字节；请为本地前台 fetch 增加 --byte-ranges 选项，值为以逗号分隔的 start:length 列表，其中 start 是从零计数的源字节起点，length 是字节数，二者均按十进制有符号 64 位整数范围解析。结果是一个紧凑文件，按列表顺序逐段写入所选内容，不保留源文件中的空隙；窗口可以乱序、重叠或重复，每次出现都占据独立的输出位置，例如 8:4,0:2,8:4 的内容依次为源文件的第 8 至 11 字节、第 0 至 1 字节及再次出现的第 8 至 11 字节。该选项只用于一个本地 HTTP(S) URL，不接受镜像列表、多个 URL 或显式远程 host，也不能与 --resume 同时提供；这些不兼容组合应在开始下载前报参数错误。源必须能确认总长度且支持字节范围请求；空列表、格式错误、负起点、非正长度、数值或输出总长度溢出，以及超过源长度的窗口均须明确拒绝，不能留下已接受的下载记录或工作文件。下载应继续使用现有并发配置与重试策略；窗口内的任务拆分、转交和完成先后不能改变输出次序，也不能把源文件的绝对位置当作输出位置，除元数据探测外的数据请求只能覆盖所选窗口，不允许先下载整文件再裁切。进度中的已完成字节、总字节及最终回执的产物大小均以紧凑文件为准，重复选择的源字节按各自输出位置计入，重试同一输出位置不能重复累加，也不能在仍有缺口时宣告完成。某次范围响应提前结束后，后续重试应继续该窗口尚未完成的内容，已经写入的其他窗口不能被覆盖成错误位置；服务端忽略 Range 返回 200，或 206 的 Content-Range 与请求区间或已确认的源总长度不符时，应报错并保留未发布状态，不得回退为整文件下载或给出成功产物回执。通过现有 --timeout 保存检查点退出后，使用返回的会话 ID 在新进程执行 fetch --resume，应在源内容不变的条件下恢复原选择次序和窗口内的剩余部分，复用已确认写入的内容，无须再次提供区间；恢复结果和进度应与一次完成该选择下载相同。未使用新选项的普通 fetch 及旧会话恢复仍按原来的整文件语义工作，原有范围不支持时的单流回退、SHA-256 校验和目标文件防覆盖行为保持兼容。

**Assistant**

我先了解一下项目结构和现有代码，再制定实现计划。

toolName: view_folder

status: success

f:\swe\099926\project-01

这是一个 Go 编写的下载管理器项目。让我深入阅读关键文件来理解现有架构。

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\fetch.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\resume.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_session.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\types\models.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\manager.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\scheduler\scheduler.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\scheduler\manager.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\concurrent\downloader.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\concurrent\worker.go

让我继续查看 probe、progress、store 以及产物发布相关代码。

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\probe\probe.go

toolName: view_files

status: failed

filePath: f:\swe\099926\project-01\internal\orchestrator\artifact.go

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\probe\probe.go

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\progress\progress.go

这是一个大型功能。让我建立任务清单并继续研究关键的持久化与事件机制。

toolName: todo_write

status: success

Todos updated: 7 items

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\store\state.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\events.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\concurrent\task.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\events.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\progress\bytes.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\pause_resume.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\testutil\mock_server.go

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\root_downloads.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\types\errors.go

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\types\config.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_test.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\progress\bitmap.go

再看几个关键文件：任务队列、fetch 进度、服务结果获取。

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\concurrent\task_queue.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_progress.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\single\downloader.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\progress\session.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\progress\progress.go

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\cli_test.go

架构已研究清楚。查看错误码定义后开始实现。

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\clioutput\errors.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\service\result.go

研究完成，现在开始实现。先更新任务清单。

toolName: todo_write

status: success

Todos updated: 7 items

**第一步：扩展 types 层**——新增 `ByteRange`、为 `Task` 增加目标偏移（兼容旧 gob 数据）、为 `DownloadRecord` 增加区间计划字段。

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\types\models.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\types\models.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\types\models.go

changes: undefined

**第二步：store 层 gob 持久化新字段。**

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\store\state.go

changes: undefined

toolName: edit_file_search_replace

status: failed

filePath: f:\swe\099926\project-01\internal\store\state.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\store\state.go

changes: undefined

**第三步：实现 compact 下载策略。** 先确认位图 `RecalculateProgress` 如何使用 task 偏移。

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\progress\bitmap.go

查看 concurrent 包中的可中断睡眠等辅助函数是否可复用。

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

现在创建 compact 策略包：

toolName: Write

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

修正 `fetchRange` 读取循环中的变量作用域与提前 EOF 处理：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

清理未使用变量：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

**第四步：接入调度器 `RunDownload`。** 先调整 compact Downloader 签名以接收检查点位图：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

现在修改 `RunDownload`，增加 compact 分支（禁止单流回退）：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\scheduler\manager.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\scheduler\manager.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\scheduler\manager.go

changes: undefined

**第五步：编排层接线。** 修改 orchestrator 的 `DownloadRequest`、探测校验与记录构建：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\manager.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\manager.go

changes: undefined

现在改造 `enqueueNew`：compact 模式禁止探测乐观回退，并在预留工作文件前强校验：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\manager.go

changes: undefined

更新磁盘预检大小与 `buildDownloadRecord`：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\manager.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\manager.go

changes: undefined

更新事件层的快照校验，使其识别 compact 双偏移任务：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\events.go

changes: undefined

更新 resume 路径，确保区间计划随冷/热恢复传递：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\pause_resume.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\pause_resume.go

changes: undefined

**第六步：CLI 层。** 先创建解析器：

toolName: Write

status: success

filePath: f:\swe\099926\project-01\cmd\byte_ranges.go

changes: undefined

现在修改 `fetch.go` 接入标志与兼容性校验：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch.go

changes: undefined

更新 cmd 层 `DownloadRequest` 结构体与校验函数：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\root_downloads.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\root_downloads.go

changes: undefined

先编译检查目前的改动：

toolName: Shell

status: success

toolName: Shell

status: success

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

toolName: Shell

status: success

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\manager.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\orchestrator\manager.go

changes: undefined

toolName: Shell

status: success

编译通过。运行 gofmt 和 vet：

toolName: Shell

status: success

现在编写测试。先看现有的 timeout/checkpoint 测试模式：

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\autoresume_test.go

查看 `waitItem` 结构与 `runCommand` 退出码约定：

toolName: search_by_regex

status: success

现在编写测试。先是解析器单元测试：

toolName: Write

status: success

filePath: f:\swe\099926\project-01\cmd\byte_ranges_test.go

changes: undefined

检查 store 初始化行为，确认拒绝路径的副作用：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\store\db.go

toolName: search_by_regex

status: success

现在编写端到端集成测试：

toolName: Write

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

修正测试服务器结构（嵌入 httptest.Server，删除无用类型）：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: search_by_regex

status: success

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: Shell

status: success

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: Shell

status: success

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: Shell

status: success

两个问题：测试用例本身写错了（大数是 start 而非 length），且本机 `httptest.NewServer` 绑定失败——项目有自定义的 `NewHTTPServerT`。查看其实现：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\testutil\http_server.go

改用项目的 IPv4 测试服务器，并修正溢出测试用例：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\byte_ranges_test.go

changes: undefined

toolName: Shell

status: success

解析器测试全部通过。网络测试因当前沙箱 Winsock 不可用而跳过——验证一下现有测试是否同样受环境影响：

toolName: Shell

status: success

toolName: Shell

status: success

环境确认无法绑定 TCP 端口（既有网络测试同样全部 SKIP），集成测试会在 CI 中运行。同时我发现 compact 下载器缺少队列关闭监视器（并发包由 completion monitor 负责），立即补上：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

现在为 compact 包的纯逻辑与 Task gob 兼容性补充无网络单元测试：

toolName: file_search

status: success

toolName: Write

status: success

filePath: f:\swe\099926\project-01\internal\types\task_test.go

changes: undefined

toolName: Write

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader_test.go

changes: undefined

toolName: Shell

status: success

测试断言写错了——小输出时 chunk 为 MinChunkSize（2MiB，已对齐），任务会按窗口长度自行截断。修正断言：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader_test.go

changes: undefined

现在修复一个重要的恢复一致性问题：调度器级重试时（同进程），concurrent 策略会通过 `store.LoadState` 自行恢复快照，compact 也应如此：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

toolName: Shell

status: success

toolName: Shell

status: success

toolName: Shell

status: success

全部通过。运行完整测试套件确认无回归：

toolName: Shell

status: success

全部测试通过。现在做一轮边界审计，补一个安全网：当快照里剩余任务为空但进度未满时（极端竞态），应从位图重建未完成区间，而不是保存看似完成的快照：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

为新辅助函数补单元测试：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader_test.go

changes: undefined

toolName: Shell

status: success

运行 vet 与 gofmt 全面检查：

toolName: Shell

status: success

toolName: Shell

status: success

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: Shell

status: success

`cmd/add.go` 是既有文件、我未改动，其格式问题保持原样。重新 vet 确认我的代码已干净：

toolName: Shell

status: success

toolName: Shell

status: success

vet 全部通过。更新现有 fetch 命令文档（仅编辑既有文件）：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\docs\reference\commands\fetch.md

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\docs\reference\commands\fetch.md

changes: undefined

现在完整重读 compact 下载器做深度自审：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

发现一个错误路径快照的健壮性问题：当联合错误包含 context.Canceled 时会跳过快照。改进为非暂停错误一律保存快照（暂停路径已在前面处理）：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

再加固快照下载量口径：剩余为零但验证进度未满时，以验证进度为准，避免虚报完成：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

复查 `RunDownload` compact 分支的完整上下文：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\scheduler\manager.go

重新构建并运行全部相关测试与格式检查：

toolName: Shell

status: success

全部通过。再探测一次网络栈是否恢复，以便运行真实集成测试：

toolName: Shell

status: success

网络栈在沙箱中持续不可用（既有测试同样 SKIP），集成测试将在 CI 环境运行。复查最终的 `fetch.go` 校验流程：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\fetch.go

对并发新代码运行 race 检测：

toolName: Shell

status: success

toolName: Shell

status: success

toolName: Shell

status: success

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

changes: undefined

toolName: Shell

status: success

toolName: Shell

status: success

所有检查通过。更新进度并做最终全量验证：

toolName: todo_write

status: success

Todos updated: 7 items

toolName: Shell

status: success

gofmt 列出的是整个目录。确认我实际改动的三个文件是否干净，以及其余是否为既有问题：

toolName: Shell

status: success

我改动的文件全部 gofmt 干净；目录中其余为既有格式差异（stash 验证：未改动时也有 26 个）。确认改动已恢复并重新构建：

toolName: Shell

status: success

最终全量测试：

toolName: Shell

status: success

全部 27 个包通过。查看最终改动统计并做收尾检查：

toolName: todo_write

status: success

Todos updated: 7 items

toolName: Shell

status: success

清理测试服务器中未使用的计数字段：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

编辑出错，查看当前处理函数并修复：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

重写整个处理函数恢复正确逻辑：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\099926\project-01\cmd\fetch_byte_ranges_test.go

changes: undefined

toolName: Shell

status: success

最后通读 compact 下载器剩余部分确认一致性：

toolName: view_files

status: success

filePath: f:\swe\099926\project-01\internal\strategy\compact\downloader.go

最终验证（构建、vet、受影响包测试与 lint）：

toolName: Shell

status: success

实现完成，所有构建、vet、lint 与测试均通过。下面是完整交付说明。

## 实现总结

### 新增文件
| 文件 | 作用 |
|---|---|
| `cmd/byte_ranges.go` | `--byte-ranges` 规格解析器：逗号分隔 `start:length`，`strconv.ParseInt(10, 64)` 严格按十进制有符号 64 位解析，拒绝空列表、畸形、负起点、非正长度及求和溢出 |
| `internal/strategy/compact/downloader.go` | 紧凑文件下载策略（约 760 行） |
| 4 个测试文件 | 解析器、gob 兼容、策略纯逻辑、CLI 端到端集成测试 |

### 核心设计：双偏移任务模型
- `types.Task` 新增 `DstOffset`（输出位置），`Offset` 仍为源位置；gob 编码升级为 24 字节并**兼容旧 16 字节检查点**，普通整文件下载语义零改动。
- 每个任务携带独立的源偏移→输出偏移映射：HTTP `Range` 用源偏移，`WriteAt` 与进度位图用输出偏移。乱序、重叠、重复窗口各自占据独立输出位置，绝不把源绝对位置当输出位置。

### 需求逐条落实
1. **参数门禁（下载前报错，零残留）**：与 `--resume`、显式 `--host`、多 URL、镜像列表互斥；窗口越界在 probe 之后、预留工作文件/写 DB 记录之前拒绝。
2. **严格 Range 契约**：compact 模式禁止 probe 乐观回退；数据请求遇到 200 或 `Content-Range` 与请求区间/已确认源总长不符即终态错误，**不回退单流、不发布产物、不给成功回执**；除元数据探测外所有请求只覆盖所选窗口。
3. **复用既有并发/重试体系**：沿用 `RuntimeConfig` 的 workers、MinChunkSize、缓冲、`MaxTaskRetries`、限流器与 `TaskQueue`；提前 EOF 后按游标续传窗口剩余部分，已写入的其他窗口不受影响、不重复计数。
4. **进度口径**：`TotalSize`/进度/回执产物大小一律为紧凑文件长度；位图以输出位置为键，重复源字节按每个输出位置计数，存在缺口时绝不宣告完成。
5. **断点续传**：检查点 gob 持久化区间计划、源总长与双偏移剩余任务；新进程 `fetch --resume SESSION` 无需再传区间即可恢复原选择次序；同进程调度器重试也会自动收养磁盘快照。
6. **兼容性**：普通 fetch 与旧会话保持整文件语义，单流回退、SHA-256 校验、目标防覆盖逻辑均未改动。

### 验证情况
- 全部 27 个包 `go test ./...` 通过，`go vet`、`golangci-lint` 0 问题，`-race` 干净。
- 本机沙箱 Winsock 无法绑定端口，**所有联网测试（含项目既有测试）均按统一约定 SKIP**；新增的集成测试覆盖紧凑内容/重叠重复、参数校验、越界无残留、200/错误 Content-Range 拒绝、提前 EOF 重试、超时检查点跨进程恢复等场景，将在 CI 网络环境中实际执行。
