交付质检对应 surgedm-surge-8ba94984-a429-44ce-b14f-dc3285db47e1，原模型 Session、144轮和冻结的7条 Rubric 全部保持。只新增质检证据，没有修改模型生产代码。基线为 9ac5b74603d0a9451305c52bd5cde99b4d93b77a，质检构建来源为 0830c1c785e33d0c6adf13d07eda1dab171515e9，其非 evidence 文件与模型末态的546个原始哈希完全一致。

可运行验证使用 Go1.26.1、Node24.21.0 和操作系统文件系统。普通 go build 所需 Go 模块是构建依赖；核心验证不下载包分发或远程数据。两个 Node 脚本用 SHA256 固定种子在内存生成16MiB源，独立拼接所选窗口作为字节级 oracle，启动仅监听127.0.0.1的受控 HTTP 服务，无转发或公网回退。

本轮实际执行 go build -o F:/swe/work/surge-delivery-qc/surge-review.exe .；go-tests-final.jsonl 是禁用缓存的六包 go test -count=1 -json 结果，698项测试通过、117项跳过、0失败。跳过项不作为可运行依据，真实 CLI 和 Node HTTP 验证补足网络入口。

实际命令为 node F:/swe/work/surge-delivery-qc/verify.mjs 和 node F:/swe/work/surge-delivery-qc/verify-once.mjs，执行脚本与交付脚本仅有工作目录、当前二进制、基线二进制三个环境变量接口差异，默认路径相同。正常及兼容验证81项全部通过，包含33个真实子进程以及3次独立进程的超时恢复。每次恢复只传7995445剩余字节，复用393216已确认字节；完整输出8388661字节。范围错误四例使用实际 CLI、真实状态码和头部，不替换生产函数。四个原始失败断言均表示 Rubric6 未通过；verify-once 驱动保存断言失败但自身退出码为0，不能以该驱动退出码宣称模型符合契约。

网络限制作用于全部真实 fetch 子进程的生产 HTTP transport。每个子进程继承 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY 及对应小写变量，指向仅拒绝请求的127.0.0.1代理；代理对 HTTP 返回403，对 HTTPS CONNECT 返回403，代码没有转发逻辑。NO_PROXY 仅允许127.0.0.1和localhost本地fixture。internal/transport/network.go:149默认使用 ProxyFromEnvironment，probe、compact、concurrent、single都经过该 transport；每个场景使用全新APPDATA及XDG配置、状态、运行目录，未加载作者设置或检查点覆盖代理。实际当前二进制对 http://example.com/qc-public-denial 和 https://example.com/qc-public-denial 的请求均进入拒绝代理，退出码分别1和8，无成功产物；network-restriction.json保存真实GET和CONNECT拒绝日志，verification.json保存同一子进程stdout/stderr和PID。不是只声明offline变量。其他桌面应用的网络未受此验证代理限制。

复现时创建全新可写目录，设置 SURGE_QC_WORK 指向该目录、SURGE_QC_BINARY 指向为当前操作系统构建的模型二进制、SURGE_BASELINE_BINARY 指向从登记基线构建的原二进制，再运行本目录 verify.mjs 与 verify-once.mjs。基线源在保留的根部.git中可检出，先完成常规构建依赖安装；两个脚本自身不读取Go缓存或在线包分发。Node fixture和拒绝代理随脚本启动并关闭，可在Linux上通过这三个路径变量使用相同协议。已有场景目录不能复用，否则目标防覆盖会正确拒绝已有产物。

通过结论覆盖参数拒绝和零工作文件、并发有序/重叠/重复拼接、紧凑进度、短流剩余范围重试、三次冷恢复和普通整文件/单流回退/SHA256/目标防覆盖/旧检查点兼容。未通过结论精确对应原Prompt对错误Range响应不得发布的承诺：先完成65536字节窗口，再首次返回200或错误起点/终点/源总长206，随后服务端恢复合法响应，调度器重新入队，四例均退出0并返回131072字节成功产物。最终字节哈希正确，失败原因是协议错误后仍成功发布，不是数据损坏。详细逐项报告和唯一最终真实终端区域图仍在上一级evidence目录，截图结论和三处逐项字段完全一致。

完整原始单次轨迹、扫描轮数绑定和renderer时长行证据已经核对；只核验144轮，未执行计数脚本。时长脚本使用完整Session重新核对为3044秒、否，renderer起止行852与10433。严格YAML由工作台和实际提交共同调用的 buildVerifyRubricYaml 产生并实际parseDocument解析，顺序/类型/文本与冻结Rubric一致。远程预检刷新匹配当前Repo与Prompt，状态PRECHECK_PASSED、item9950、batch02f8be46e6ef4f77bb9a1a7ef88e70ed。Fork API再次核验isFork=true、parent=SurgeDM/Surge、viewerPermission=ADMIN。

初次验证命令把实际目录internal/orchestrator误写为internal/lifecycle，原始失败保留在go-tests.jsonl；已改用实际六包命令重新完整执行，最终结果见go-tests-final.jsonl。初次检查脚本额外查找根部instruction.md，随后改为核对原轨迹User原文、冻结review-inputs及元数据和轨迹哈希；题面与冻结Rubric没有变更。
