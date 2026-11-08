# Surge 有序字节窗口交付复盘

对象：surgedm-surge-8ba94984-a429-44ce-b14f-dc3285db47e1。结论：部分完成，Rubric 1–5、7 通过，6 未通过。144 轮沿用扫描阶段的 Trae 原始消息统计，收录为长程题。仅审查登记 Session 的模型最终代码，不修复其生产实现。

## 输入门禁

开始和整理证据时 syncRevision 均为 5；基线为 9ac5b74603d0a9451305c52bd5cde99b4d93b77a。review-inputs.json 保存冻结 Prompt、7 条原顺序 Rubric、完整 Session 与 effectiveRoundsEvidence。完整轨迹 trajectory.md 及扫描 metadata 已读取，source 为桥原始消息，计数 source 为 count-trae-cn-steps.py、scope latest_turn、effectiveRounds 144，与 SQLite rounds 和 Harness 相同，未重新计数。GitHub API 核验登记 Fork 为 zhanglei10281852-gif/Surge，parent SurgeDM/Surge，viewerPermission ADMIN。

当前 Go 1.26.1 满足 go.mod 的 1.26.0。基线二进制的 Go 构建信息具有同一完整 vcs.revision 且 vcs.modified=false；复盘二进制具有同一 vcs.revision 且 vcs.modified=true。production-hashes.json 绑定实际审查与运行的生产文件。项目根 .git 保留。

## 实际调用图、状态和提交边界

cmd/fetch.go:63 的 flag Changed 检查先调用 byte_ranges.go:17 按十进制 int64 解析，并检查空列表、负起点、非正长度及求和溢出。fetch.go:70–103 在创建 fetchOwner 前拒绝 resume、显式 host、多 URL、镜像。fetchOwner 构造独立前台 session、文件锁、store、生命周期管理器及事件 worker。入口并不创建远端模型任务。

runFetch → EnqueueWithID → LifecycleManager.enqueueNew。manager.go:380–401 在确认源大小与范围支持后检查每一窗，使用减法形式防止源边界加法溢出。:445 才独占预留 .surge 工作文件，:485 在调度器 AddWithPreparation 准入边界内写接受记录。被拒绝参数及源元数据不满足者均在此之前返回，正常输出目录无工作文件。会话元文件不表示下载已被接受。

buildDownloadRecord 复制按出现顺序的 ByteRanges、SourceSize 和 compactTotal。scheduler/manager.go:198 选择 compact 策略；默认整文件在 :210/:298 继续原 concurrent/single 分支。compact 策略不进入整文件 fallback。

compact/downloader.go:284 在每窗内拆任务，源 Offset 用于 HTTP Range，累计的 DstOffset 用于磁盘 WriteAt 和 bitmap。不同窗的输出任务不重叠；重复源窗拥有不同 DstOffset。任务计划在执行前构造，active 游标由所属 worker 推进；快照读取受 mutex 保护。既有 InitialConnectionCount、最大连接数、最小块和 worker 缓冲配置继续适用。队列分配与 HTTP 完成先后不会改变任务的输出位置。

fetchRange:405 创建受同一 download context 控制的请求并强制 identity 编码；:432 拒绝数据 200，:448 校验 Content-Range 起点、终点、已确认总长。:466 成功 WriteAt 后才更新输出 bitmap、Downloaded，并同时推进源/输出游标。短响应留下剩余任务，worker 仅重试游标之后的源区间。进度按互不重叠的输出位置累计；完成守卫 :254 要求 VerifiedProgress 等于 compactSize。

## 真实可控交错和停止边界

1. 乱序完成：Node HTTP 服务分块延迟多个窗口响应，实际数据请求并发，产物 8388661 字节且与独立拼接哈希一致；请求均在选窗内。
2. 传输中断：只对非探测首个数据响应发送 70001 字节后断开连接。日志记录真实 short 标记，下一次 Range 从原源起点加 70001 发起，同一任务末端不变；已完成的其他输出窗内容保持正确。
3. 超时发生在写入后：每个活动窗口发送 128 KiB 后保持 HTTP 响应不结束，由 CLI 的 1200ms timeout 触发暂停。compact 等待所有 worker 退出，再 drain 剩余队列、收集源/输出双游标，持久化选择计划和 bitmap。saveSnapshot 设置 pending snapshot，直接写 store，再发送暂停事件；同一事件 worker 串行处理持久化事件。fetchOwner.close 先 manager.Shutdown，再等待事件 worker 停止，核对最终记录和 checkpoint，然后释放 store 和 session lock。三次真实独立进程均确认 393216 字节检查点，最终文件不存在；新 PID 仅带 session ID 恢复，传输量准确等于 8388661−393216=7995445，最终字节完全相同。
4. 协议错误发生在一窗完成后：窗口一 bytes=1024-66559 先正常结束；窗口二 bytes=8388608-8454143 延迟后首次分别返回 200 或起点、终点、总长错误的 206。worker 终止本层重试并返回 ErrRangeContract，保存尚未完成任务。scheduler.shouldRetryFailedDownload 未排除此 sentinel，:926 将任务重新入队。服务端随后正常响应同一区间时，四例均返回 exit=0、ok=true、artifact.size_bytes=131072，并真实发布文件。这不是缺失字节或内容破坏；实质缺口是协议错误被上层重试消除，违反强制失败和未发布契约。详见 verification-once.json、contract-raw-summary.txt。

下载响应均在 fetchRange defer 中关闭；所有数据 worker 由 WaitGroup 等待后才保存快照或返回；工作文件在 Download 返回时关闭，scheduler 才能处理其返回。队列监视 goroutine 只操作队列，不持有文件/HTTP body；完成或 download context 取消后退出。外层 fetchOwner 等待调度器、事件 worker 和锁释放后返回可确认的检查点。此任务不涉及对象移除重建、订阅晚绑定或外部 API 事务，不额外引入这些验收义务。

## 发布和兼容

物理下载成功后 scheduler 才执行 SHA 校验并发 EventComplete。LifecycleManager 的事件 worker 在 events.go:352 调用 finalizeArtifact，发布失败则写 error，发布成功后才记录 completed。CLI GetResult/关闭核对产生最终回执。协议错误被调度器当可重试错误处理，重试成功后进入这条正常发布路径，构成 Rubric 6 的直接失败证据。

store/state.go 显式 Gob 投影新增 ByteRanges/SourceSize，types.Task.GobDecode 接受旧 16 字节和新 24 字节数据。冷恢复 buildResumeConfig 从保存记录恢复选择、源大小、双偏移任务和输出 bitmap。未带新选项时继续旧源坐标语义。真实登记基线二进制生成部分写入检查点，再由当前二进制新进程恢复，完整 16777216 字节与源哈希相同且确认复用旧字节；默认 whole-file、无 Range 单流、正确 SHA、错误 SHA（exit 7）、已存在目标防覆盖（exit 7）均有真实二进制结果。

## 验证证据及限制

go test -count=1 -json ./cmd ./internal/strategy/compact ./internal/orchestrator ./internal/scheduler ./internal/store ./internal/types 的六包完成，698 测试项通过、117 测试项跳过，无失败项。跳过项含 HTTP 监听不可用的模型新增集成测试，因此没有将包级绿色输出当作这些行为已验证。独立 Node HTTP 服务可在当前环境监听，驱动相同生产入口二进制完成 95 项正常、参数、永久协议错误和兼容检查。verify-once 的四项拒绝断言实际失败，按原始回执判 Rubric 6 未通过。

最初验证的快速传输没有输出第二条 plain 进度行，45 秒脚本保护亦早于调度器十次重试结束；verification-initial.json 保留这一验证器记录。延长流式响应与保护时限后的 verification.json 实际显示紧凑进度且等待取得协议失败回执。上述保护中止不作为模型失败理由。

复现：在项目目录 go build -o F:/swe/work/surge-delivery-review/surge-review.exe .，随后 node evidence/verify.mjs 和 node evidence/verify-once.mjs。脚本使用隔离 APPDATA 和临时端口；重新运行时更换 fixtures 目录后缀，避免现有目标防覆盖改变结果。基线程序必须来自登记 SHA 的干净 checkout。verify-rubric.yaml 由工作台同一 buildVerifyRubricYaml 序列化，并实际解析验证全部 7 项的唯一正整数 ID、id/type/text 字段顺序、f2p/p2p、冻结原顺序和原文本。

时长证据单独保存：完整 Session 调用 measure-trae-session-duration.mjs，renderer Realtime created_at 到 completed updated_at 为 3044 秒，exceededTwoHours=否；renderer 行号 852、10433。时长不影响完成度与收录。
