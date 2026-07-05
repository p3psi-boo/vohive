# VoHive Web UX 优化 Checklist

> 来源：2026-07-05 对 `web/src` 的 UX 审查。所有路径相对于 `web/src/`。
> 技术栈：Vue 3 + Vite + TypeScript + TailwindCSS + Element Plus + Pinia。
> 行号为审查时的近似值，动手前请先重新定位代码。
> 每项任务独立可交付；完成后勾选并附 commit hash。

---

## P0 — 风险修复（误操作 / UI 错误）

- [x] **1. 删除国家规则加确认对话框**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Proxy.vue` ~896 行（模板中"删除规则"按钮）及 ~466-476 行（`doDeleteCountryRule`）。
  - 现状：按钮直接调用 `doDeleteCountryRule(rule.country_code)`，无任何确认，一次点击即删除，不可撤销；删除后该国家会"恢复直连"，UI 中也没有提示这一后果。
  - 要求：仿照同文件删除前置代理（~420 行）的写法，用 `ElMessageBox.confirm` 加确认，文案需说明"删除后该国家流量将恢复直连"。
  - 验收：点击删除先弹确认；取消不发请求；确认后有成功/失败 toast。

- [x] **2. 修复 SMS 消息删除按钮可能双重渲染**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Sms.vue` ~914-945 行，消息气泡列表内两个删除按钮。
  - 现状：两个按钮的 `v-if` 条件分别为 `!isNarrowLayout && m.type === 2 && m.device_name` 与 `!isNarrowLayout && (m.type !== 2 || !m.device_name)`。逻辑意图应是互斥（发送消息 vs 接收消息两种布局位置），但需核实对"type===2 且有 device_name"的消息是否真的互斥、以及两个分支除位置外是否还有其他差异。
  - 要求：核实并收敛为单一清晰条件（例如抽 computed / 单个组件按 `m.type` 决定对齐方向），确保任何消息只渲染一个删除按钮。
  - 验收：对发送成功/发送失败/接收三类消息各验证一次，删除按钮均只出现一个且功能正常。

- [x] **3. 加强"删除设备配置"的防误触**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Devices.vue` ~921-928 行（设备删除，目前仅 `ElMessageBox.confirm`）。
  - 参照：`components/DeviceEsimTab.vue` ~309-323 行删除 eSIM Profile 时用 `ElMessageBox.prompt` + `inputPattern` 要求输入 ICCID 后 4 位。
  - 现状：删除 eSIM Profile 的确认强度反而比删除整台设备配置更高，属于强度倒挂。
  - 要求：设备删除改为输入设备 ID（或其末尾若干位）确认，文案说明删除的具体影响范围（配置是否可恢复、是否影响运行中的代理实例——需查后端行为，入口在 `stores/devices.ts` 或 `services/`）。
  - 验收：输入不匹配时确认按钮不可提交；流程与 eSIM 删除风格一致。

- [x] **4. 修复前置代理编辑时密码字段静默清空的困惑**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Proxy.vue` ~372-374 行：编辑时 `if (upstreamForm.value.password === '****') upstreamForm.value.password = ''`；"留空会保持原密码"的提示在 ~849 行，离输入框太远。
  - 要求：把提示直接放到密码输入框上（placeholder 设为"留空保持原密码"，或字段下方 helper text），不要依赖远处的说明文字。
  - 验收：编辑已有代理时，密码框内即可看到"留空保持原密码"的提示；留空提交后原密码确实保留。

---

## P1 — 一小时级清理（冗余删除）

- [x] **5. 删除死代码：`components/HelloWorld.vue`**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - Vite 脚手架残留，grep 全仓库无引用。直接删除文件，跑一次 `cd web && npm run build`（或 `make build-local`）确认无引用报错。

- [x] **6. 处理未使用的 `composables/useSmartPoll.ts`**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - 现状：与 `composables/usePollingScheduler.ts` 功能重叠 70%+（差异：useSmartPoll 在 visibilitychange 时主动 trigger、无后台降频；usePollingScheduler 有 `backgroundIntervalMs` 后台降频，被 Dashboard/Devices/Proxy/Sms 使用）。useSmartPoll 无任何引用。
  - 要求：删除 useSmartPoll；若其"回到前台立即刷新"的行为有价值，将该能力作为选项合并进 usePollingScheduler（其 ~82 行 `onVisibilityChange` 目前是空实现，有注释"不再立即抢占"，合并时注意尊重该设计决定或与维护者确认）。
  - 注意：`tests/` 与 `composables/useSmartPoll` 相关的 typecheck 文件若存在一并处理。

- [x] **7. 抽取侧边栏重复模板为组件**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`layouts/AuthenticatedShell.vue`，桌面版 `<el-aside>`（~127-166 行）与移动端 `<el-drawer>`（~168-205 行）内的品牌区 + `el-menu` + 底部用户卡片为逐字重复的两份。
  - 要求：抽成 `components/SidebarContent.vue`（props: `collapsed`），两处复用；scoped 样式（`.sidebar-*` 系列，~246-417 行）随组件迁移，注意 `:deep()` 选择器与 `:global(html.dark)` 变量覆盖要保持生效。
  - 验收：桌面折叠/展开、移动端抽屉、暗色模式三种形态视觉与现在一致。

- [x] **8. 处理顶栏装饰性绿色脉冲圆点**（已完成；选择 a 删除；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`layouts/AuthenticatedShell.vue` ~221-226 行，header 右侧一个永远为绿色的 ping 动画圆点，不绑定任何真实状态。
  - 要求（二选一）：a) 删除；b) 绑定真实健康状态（可复用 `debug/collector.ts` 收集的 API 错误事件或 SSE 连接状态，参考 `composables/useEventStream.ts`），并加 tooltip 说明含义，异常时变红/黄。
  - 验收：不再存在"永远绿色但无含义"的指示灯。

- [x] **9. 侧边栏底部用户卡片去硬编码**（已完成；auth store 当前未保存真实用户名，已简化为退出登录按钮；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`layouts/AuthenticatedShell.vue` ~152-165 与 ~190-203 行（做完任务 7 后只剩一处）。
  - 现状：显示写死的 "Admin / Administrator"，头像用的是 Settings 图标，唯一有用的是退出按钮。
  - 要求：从 `stores/auth.ts` 读取真实用户名（如果 auth store 没有存，评估是否值得补；不值得则把卡片简化为单个"退出登录"按钮），头像换成用户/人形图标或用户名首字母。

---

## P1 — 一致性统一

- [x] **10. 建立并应用中文术语表**（已完成；新增 `web/docs/glossary.md` 并替换展示文案；验证：`cd web && npm run build` + 旧词 grep；commit: 待提交）
  - 需统一的冲突（均为审查中实际发现）：
    - `views/Proxy.vue`：「代理」vs「实例」——Tab 标签"本地出站代理"(~520)、卡片标题"本地出站实例"(~635)、Drawer 标题"代理实例"(~707)；按钮"新增代理"(~554，前置代理) vs"新增实例"(~641，出站)。建议：出站统一叫「代理实例」，动作按钮统一「新增实例」/「新增前置代理」。
    - `views/Sms.vue`：「会话」(~803, ~869) /「对话」(~688) /「联系人列表」(~809) 混用。建议统一「会话」。
    - 设备操作：「切换 IP」（`components/DeviceDetailHeader.vue` ~45）vs「IP 轮换」（`views/Devices.vue` ~769 附近文案）。统一其一。
    - `components/DeviceEsimTab.vue` ~504 行 `ESIM` 应为 `eSIM`。
    - 设备状态文案：「当前设备未运行」(`DeviceAtTab.vue` ~24) vs「设备当前未启动」(~30)。
  - 要求：在 `web/README.md` 或新建 `web/docs/glossary.md` 里记录选定术语，然后全局替换。只改展示文案，不改代码标识符/API 字段。
  - 验收：`grep -rn` 上述旧词在 `web/src` 的模板文案中不再出现。

- [x] **11. Settings 通知配置文案去中英混杂**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Settings.vue` 通知配置区（~445-837 行）。
  - 实例：`Username`/`Password`（Email 区）应为「用户名」「密码」；`Bot Token`/`Chat ID`/`Admin ID`、`App ID`/`App Secret`、`Group IDs (群聊)`、`发件人地址 (From)`、`Token`/`Topic`/`Channel` 等。
  - 规范：表单 label 全中文（必要时中文后保留原文括注一次即可，如「机器人令牌（Bot Token）」）；placeholder 放英文格式示例。技术上无歧义的缩写（SMTP、SSL/TLS、Webhook、URL）可保留英文。
  - 验收：通知配置 6 个渠道 tab 的 label 风格统一。

- [x] **12. 修改删除确认文案，避免技术黑话**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - `views/Sms.vue` ~654 与 ~687 行：「仅删除短信中心历史记录」→ 改为明确说明：「删除后无法恢复；此操作仅删除本系统中的记录，不影响对方手机上的短信」。
  - `views/Settings.vue` ~102 行：toast「通知配置已保存（已写入 config.yaml）」→ 去掉文件名细节，改为「通知配置已保存」。

---

## P2 — 反馈与状态改进

- [x] **13. Settings 页面加脏值追踪 / 未保存提示**（已完成；通知表单加载/保存后刷新快照，修改后显示未保存并拦截路由离开；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Settings.vue`。通知配置为一个整体"保存通知配置"按钮（~445 行），密码修改独立（~350 行）。
  - 现状：无脏值追踪，改完切路由直接丢失。
  - 要求：对通知配置表单做快照比对（加载后 deep clone 一份初始值），有改动时：a) 保存按钮高亮/显示"未保存"标记；b) `onBeforeRouteLeave` 弹确认。
  - 验收：修改任一字段后切路由会被拦截提示；保存后不再提示。

- [x] **14. 补齐所有通知渠道的"测试通知"按钮**（已完成；Telegram/飞书/QQ/Pushplus 增加后端测试端点、服务/store 方法与设置页按钮；验证：`cd web && npm run build`、`nix develop -c sh -lc 'GOWORK=off go test ./internal/api ./internal/notify'`；commit: 待提交）
  - 位置：`views/Settings.vue`。现状：仅 Webhook (~576)、Bark (~640)、Email (~729) 有测试按钮；Telegram、飞书(Lark)、QQ、Pushplus 没有。
  - 前置：需查后端是否有对应测试端点（搜 `internal/` 下通知相关 handler，以及 `web/src/services/` 里现有 test 调用的 API 路径规律）。若后端缺端点，本任务需要同时补后端（Go/Gin），或先拆出后端子任务。
  - 验收：每个已启用且配置完整的渠道均可发测试消息，成功/失败均有 toast。

- [x] **15. SMS 新消息提醒**（已完成；线程轮询 diff 新接收消息后弹出可点击通知，侧边栏短信中心显示跨页面未读 badge；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Sms.vue`（轮询在 ~539-542 行，5s/后台15s，经 `stores/sms.ts`）。
  - 现状：拉到新消息无任何提示，线程列表虽有未读蓝点（~833 行）但页面外无感知。
  - 要求（最小版）：a) 轮询 diff 出新增的接收消息时，若不在对应会话内则 `ElMessage`/`ElNotification` 提示（可点击跳转会话）；b) 侧边栏「短信中心」菜单项挂未读数 badge（菜单在 `layouts/AuthenticatedShell.vue` `menuItems`，需要一个跨页面可用的未读 computed，建议放 `stores/sms.ts`）。
  - 注意：避免首屏加载时把历史未读全弹一遍——只对"本次轮询新出现"的消息提醒。

- [x] **16. 统一 Proxy 页两个 tab 的轮询策略**（已完成；tab 激活立即刷新，并注释保留不同间隔原因；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Proxy.vue`。出站代理轮询 5s/后台15s 全局启用（~274-279 行）；前置代理 10s/后台30s 且仅在 upstream tab 激活时启用（~491 行 `enabled: upPollEnabled`）。
  - 问题：切回 upstream tab 时可能看到过期数据；两套间隔无设计依据。
  - 要求：至少在 tab 激活的瞬间立即触发一次刷新（消除过期窗口）；间隔统一或在代码注释中写明差异原因。

- [x] **17. Login 错误信息区分 + 去假延迟**（已完成；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Login.vue`。~53 行失败时一律 toast「登录失败，请检查凭证」，忽略了后端返回的错误信息；~27 行有 600ms 人为延迟。
  - 要求：区分 401（凭证错误）与网络/5xx（「无法连接服务器，请稍后重试」），优先展示后端 message（注意防 XSS，用 ElMessage 纯文本即可）；删除人为延迟。
  - 加分项：「记住我」（评估 `stores/auth.ts` 的 token 存储方式后决定 localStorage vs sessionStorage 切换）。

- [x] **18. Dashboard 与 Devices 的 URL 状态残留**（已完成；非法 tab 回落并同步 overview，不存在 device 清理并提示；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Devices.vue` ~636-646 行的自动选中逻辑。
  - 现状：`?device=xxx` 指向的设备临时离线（被过滤出列表）时，URL 参数残留，页面显示空态但地址栏还指着旧设备；query.tab 传入非法值被静默忽略。
  - 要求：设备列表加载完成后，若 query.device 不在列表中，`router.replace` 清掉该参数并可选 toast「设备 xxx 当前不在线」；非法 tab 值回落到 overview 并同步 URL。
  - 验收：手动构造错误 URL（不存在的 device/tab）刷新页面，URL 被纠正且无报错。

---

## P2 — 信息密度与移动端

- [x] **19. eSIM tab 拆分"下载新 Profile"表单**（已完成；下载表单改为 Dialog，进行中/失败进度在 Dialog 外固定可见；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`components/DeviceEsimTab.vue`（868 行的大组件）。单屏堆叠：芯片信息+操作（~500-539）、按 eUICC 分组的 Profile 列表（~541-685，每行 4 按钮 + 行内折叠卡策略）、下载表单（~735-794）。
  - 要求：下载表单默认收起，改为顶部/芯片信息区一个「下载新 Profile」按钮，点击展开 Dialog 或 el-collapse。下载进度条（~774-785）在收起状态下也要可见（进行中时固定显示）。
  - 验收：手机宽度（<768px）下，不下载时一屏能看到芯片信息 + 第一个 Profile。

- [x] **20. 出站代理卡片降噪**（已完成；状态合并为 `StatusLight` + 文本，低频重启/删除收进下拉菜单，前置代理删除也收进下拉；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Proxy.vue` ~658-692 行。现状每张卡 3 行文字 + 4 个状态 tag + 多个按钮/button-group，小屏折行后卡片高度爆炸。
  - 要求：保留高频操作（启停、编辑）为直接按钮，低频操作（删除等）收进 `el-dropdown`；tag 精简（"启用"状态与运行状态 tag 语义重叠时合并为一个 StatusLight——项目里已有 `components/StatusLight.vue` 可复用）。
  - 类似处理前置代理卡片（~583-609 行）。

- [x] **21. 合并卡策略的两个编辑入口**（已完成；eSIM 行内改为只读摘要 + 去卡策略页编辑，唯一可写入口保留卡策略 tab；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`components/CardPolicyPanel.vue`（卡策略 tab，~92-214 行）与 `components/EsimCardPolicyInline.vue`（eSIM tab Profile 行内，~109-161 行）。两者提供相同的网络/VoWiFi/飞行三开关，状态独立维护。
  - 相关逻辑：`composables/useCardPolicyToggles.ts` 已存在，确认两组件是否都经由它。
  - 要求：选定一处为唯一编辑入口（建议保留卡策略 tab），另一处改为只读状态展示 + 「去卡策略页编辑」链接（切 tab 可用 Devices.vue 现有的 tab query 同步机制）。
  - 验收：同一策略不再有两个可写入口；只读处状态与编辑处实时一致。

- [x] **22. 统一敏感信息隐藏开关**（已完成；两处共用 `useSensitiveVisibility` 的 module 级 ref，并补充全局提示；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`components/DeviceOverviewTab.vue` ~323 行与 `components/DeviceEsimTab.vue` ~529-534 行各有独立的敏感信息（ICCID/IMEI 等）显示开关。
  - 现状：项目已有 `composables/useSensitiveVisibility.ts`，确认两处是否各自实例化了独立状态。
  - 要求：改为共享单一状态（module 级 ref 或 Pinia），一处切换全局生效；可考虑持久化到 localStorage。

- [x] **23. Logs 页过滤栏 sticky + 手动重连**（已完成；过滤栏 sticky、断线可手动重连、暂停文案调整、fields 提升对比度；验证：`cd web && npm run build`；commit: 待提交）
  - 位置：`views/Logs.vue`。过滤器在日志容器上方（~204-221 行），滚动后不可达；连接状态只有绿色脉冲点（~194 行）无重连按钮；`fields` 文字用 `amber-300/70`（~241 行）暗色下对比度不足。
  - 要求：过滤栏 `sticky top-0`；断线时显示「重新连接」按钮（流逻辑见 `composables/useEventStream.ts` 与 `stores/logs.ts`）；调整 fields 配色至可读对比度。
  - 顺带：确认「暂停」按钮（~177 行）语义——若同时断流，文案改为「暂停接收」。

---

## 备注（审查中发现但暂不立项）

- Sms.vue 用容器查询（`@container (min-width: 980px)`，~1167 行）、Proxy.vue 用 `max-w-7xl` + 媒体查询，两页响应式断点行为不一致——大改版时统一。
- Proxy 实例创建时 ID（`proxy-${Date.now()}`，~186 行）与端口（`10800 + instances.length`，~187 行）的自动生成逻辑对用户不可见，可能端口冲突——待与后端校验逻辑一起看。
- AT/USSD 终端错误只在气泡内展示（`DeviceAtTab.vue` / `DeviceUssdTab.vue`），可见度低，与 eSIM 的红色进度条风格不一致。
- `components/DebugPanel.vue`（Ctrl+Shift+D 唤起）面向开发者，考虑生产构建中默认禁用或权限控制。
