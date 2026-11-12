# 4158 TRACE-001 真实生产差异补证

本次只修复交付证据；未修改原模型生产实现、未重新运行模型。原完整 Session、Trae 扫描144轮、冻结 Prompt / 7条 Rubric、部分完成及 R6 未通过全部保持。

## 来源与版本

- 登记基线：9ac5b74603d0a9451305c52bd5cde99b4d93b77a
- 首次实现：0830c1c785e33d0c6adf13d07eda1dab171515e9
- 锁定的原最终提交：b3fcf12cbf2ed71c341dca50e9ec253f4bafcc29
- 原最终提交的 parent 是首次实现提交；最终提交单次差异只有19个 evidence 路径。GitHub API及无认证raw读取已核验，原最终树实际包含 compact 实现，不应把最后一次commit的delta当成基线至模型末态的全部差异。
- 原最终源码：https://github.com/zhanglei10281852-gif/Surge/tree/b3fcf12cbf2ed71c341dca50e9ec253f4bafcc29
- 原完整差异：https://github.com/zhanglei10281852-gif/Surge/compare/9ac5b74603d0a9451305c52bd5cde99b4d93b77a...b3fcf12cbf2ed71c341dca50e9ec253f4bafcc29

## 完整生产补丁

上一级 model.patch 是 git diff --binary --full-index 9ac5b74603d0a9451305c52bd5cde99b4d93b77a b3fcf12cbf2ed71c341dca50e9ec253f4bafcc29 -- . ':(exclude)evidence' 的真实输出，包含10个生产实现文件、4个测试文件、1个文档文件；没有evidence路径或嵌套旧补丁。本次生产树没有新增二进制改动。补丁75752字节，SHA-256 217a292e9313f472c4a4062505d24317ac2d198a242920b0f3ef678a8d2d88fa。原有递归补丁仅在工作台独立返修备份目录保存，不再放入生产Patch。

已在干净基线上 git apply --check 后实际应用，546个受版本控制且非evidence文件与原最终树逐字节相同；原模型清单546项哈希也全部匹配。Windows默认core.autocrlf=true会转换行尾；复现使用core.autocrlf=false以取得Git blob原字节。

新增的补证源码提交以登记基线直接为parent，生产内容与原最终树相同，单条commit的diff也能直接看到compact实现及scheduler接入，不依赖嵌套补丁或口述轨迹。该补证提交并非第二次模型运行。原锁定commit_url保留，完整补证提交URL另附返修材料。

## 可复现执行（Linux / Windows均使用相同生产代码）

先提供满足go.mod的Go1.26.1（至少1.26.0）及Node24，并在依赖安装阶段执行go mod download。核心验证自身只用Node内置库、文件系统及127.0.0.1 HTTP，无数据库、集群、云账号、设备挂载或在线分发。

在干净基线目录执行：

```sh
git -c core.autocrlf=false clone https://github.com/SurgeDM/Surge.git surge-replay
cd surge-replay
git checkout 9ac5b74603d0a9451305c52bd5cde99b4d93b77a
# model.patch 为本附件的真实补丁，切勿使用 HEAD~1
git apply --check /absolute/path/model.patch
git apply /absolute/path/model.patch
go mod download
go build -o /absolute/work/surge-model .
go test -count=1 -json ./cmd ./internal/strategy/compact ./internal/orchestrator ./internal/scheduler ./internal/store ./internal/types > /absolute/work/go-tests.jsonl
# 另建基线checkout并从基线构建surge-base，供旧检查点兼容验证
# 以下每个目录都必须新建为空，已有目标会触发正确的防覆盖拒绝
SURGE_QC_WORK=/absolute/work/normal SURGE_QC_BINARY=/absolute/work/surge-model SURGE_BASELINE_BINARY=/absolute/work/surge-base node /absolute/path/verify.mjs
SURGE_QC_WORK=/absolute/work/contract SURGE_QC_BINARY=/absolute/work/surge-model node /absolute/path/verify-once.mjs
```

根部.git全程保留。本次实际在Windows执行，对应二进制为surge-reconstructed.exe，脚本通过环境变量使用独立空目录，不依赖源码里默认F盘路径。Linux命令给出相同脚本的路径接口，没有宣称本次在Linux重新执行。

## 本次实际结果

- 从上述干净基线+补丁构建成功。Go六包禁用缓存：698项通过、117项跳过、0失败；跳过项不计入核心验证覆盖。
- 原有独立Node HTTP驱动实跑33个子进程、81项正常/兼容断言全部通过。所有fetch子进程使用每例全新APPDATA/XDG目录，生产HTTP transport经拒绝公网的loopback代理；HTTP和HTTPS公网拒绝探针均实际生效。fixture为内存确定性生成的16MiB数据，无远程数据和作者缓存。
- 三次--timeout后冷恢复，各复用393216字节，仅传7995445剩余字节，产物8388661字节且哈希相同。包含乱序/重叠/重复窗口、短响应续传、进度、非法参数、整文件、SHA-256、防覆盖与旧检查点恢复。
- R6四例仍未通过：先完成65536字节窗口，再让第二窗口首次返回200或错误Content-Range起点/终点/总长；服务端恢复正常后，调度器重试，四例exit=0、ok=true、真实发布131072字节文件。verify-once驱动自身退出0只表示取证完成，四个mustRejectAfterAnyContractViolation=false才是Rubric判定结果。

## 生产调用链和失败依据

cmd/fetch.go:63解析选择参数并拒绝不兼容组合，orchestrator/manager.go:380先检查源长度/Range和窗口边界，再在:445预留工作文件、:485调度准入后保存接受记录。scheduler/manager.go:195-205接入compact，默认路径仍走原策略。compact/downloader.go:284为每次窗口出现分配独立DstOffset；:405真实Range请求；:466 WriteAt成功后推进源/目标双游标；:254要求输出完整。:492保存选择计划、双偏移剩余任务和bitmap，store/state.go显式Gob投影持久化ByteRanges/SourceSize，pause_resume.go:495冷恢复选择。全部worker结束后才保存检查点或返回。

R6的直接链为compact/downloader.go:432/446/448抛ErrRangeContract，:357只停止worker内部重试；scheduler/scheduler.go:1093-1112的原上层重试分类未排除此错误，:926重新入队；compact:158读取已持久化剩余任务续传，成功后orchestrator/events.go:352 finalizeArtifact发布产物。scheduler/scheduler.go本身未被原模型修改，故不人为制造该文件diff；真实调度接入差异在scheduler/manager.go。失败是错误协议响应后仍成功发布，四例最终字节正确，不把它写成数据损坏。此结论与原题面明确的终态失败要求一致。

逐项结论保持：

1 通过
2 通过
3 通过
4 通过
5 通过
6 未通过 数据响应首次返回200或错误Content-Range后调度器仍重试并发布131072字节成功产物
7 通过

原唯一截图已打开视觉核验全部7条及R6原始HTTP/回执证据，仍对应同一模型生产树；原轨迹保持原文件。详细原始命令、HTTP请求、回执、测试输出与每文件哈希随本目录保存。
