# VoHive Android Agent（无屏版）

Android Agent 以常驻前台服务运行，不提供 Launcher 或原生配置界面。服务在
0.0.0.0:8765 提供带会话鉴权的首次接入与故障恢复页面；配对完成后，设备状态、
SIM/eSIM、短信与代理统一在 VoHive 后台管理。

~~~mermaid
flowchart LR
    B["Agent 本地网页"] -->|"首次接入 / 恢复"| H["0.0.0.0:8765"]
    H --> S["AgentService\n前台服务 / 保活"]
    S --> T["Telephony / SIM / eSIM / SMS"]
    S -->|"UDP 8764 自动发现"| V["VoHive Server"]
    S -->|"WebSocket 长连接"| V
    A["ADB / 系统镜像"] -->|"安装与授权"| S
~~~

## 功能

- UDP 局域网自动发现、VoHive 端一次批准配对和六位配对码兜底；
- PBKDF2-HMAC-SHA256 密码哈希、HttpOnly/SameSite 会话、CSRF 和登录限速；
- IMEI、IMSI、ICCID、手机号、固件、基带、电池、注册状态和蜂窝信号采集；
- 单卡自动选择，多 SIM/eSIM 枚举、订阅选择与 eSIM 切换；
- 短信接收、长短信发送、回执、查询、读取和删除；
- 短信/eSIM 事件持久化排队、服务端确认和断线重发；
- 将 VoHive HTTP/SOCKS5 TCP 出口绑定到选定订阅的蜂窝网络；
- 前台服务、开机启动和 WebSocket 自动重连。

Agent 本地网页不重复提供短信库、SIM 表格和代理设置，只展示连接恢复入口以及短信、
蜂窝代理、eSIM 三项结果状态。

## 构建

需要 JDK 17、Android SDK 37、Gradle 9.4.1：

~~~bash
cd android-agent
./gradlew :app:lintDebug :app:assembleDebug
~~~

APK 位于 app/build/outputs/apk/debug/app-debug.apk。

## 无屏部署与自动配对

打开无线或 USB ADB 后执行：

~~~bash
cd android-agent
ADB="$ANDROID_HOME/platform-tools/adb" \
WEB_USERNAME=admin \
WEB_PASSWORD='CHANGE_ME_12_CHARS' \
./scripts/provision-headless.sh SERIAL
~~~

脚本会安装 APK、授予普通运行时权限、申请默认短信角色、写入本地网页凭据并启动前台服务，
最后输出局域网 URL、用户名和密码。然后：

1. 在 VoHive 打开“设备 → 添加设备 → Android 手机”；
2. 等待设备自动出现；
3. 点击“允许接入”。

VoHive 会自动生成设备 ID、绑定 Agent ID、下发一次性凭据，并创建启用状态的 HTTP 与
SOCKS5 蜂窝代理。唯一活动 SIM 会自动使用；多卡设备才需要在设备页选择。

广播被 VLAN 或接入点隔离时，在 VoHive 的 Android 添加页生成六位配对码，然后只在 Agent
本地页面填写 VoHive 地址和配对码。Device ID、Agent ID 与 Token 不需要人工复制。

监听地址固定为 0.0.0.0，默认管理端口为 8765，可通过 HTTP_PORT 修改。UDP 自动发现
使用端口 8764。

ADB 首次配置通过受 android.permission.DUMP 保护的 Receiver 完成，再启动同样受保护且
NoDisplay 的 Bootstrap Activity，由它拉起非导出的前台服务；普通 Android 应用不能调用
配置或启动入口，设备上也不会出现原生界面。

未经过脚本直接启动时，本地网页使用默认凭据（账号 admin、密码 admin），
登录页只需输入密码；请在设置页尽快修改密码。

## 本地网页

打开 http://DEVICE_LAN_IP:8765/，或点击前台服务通知直接用默认浏览器打开
（指向设备自身的 127.0.0.1）。网页是 Vite + Vue 构建的 SPA（hash 路由），带
manifest 与 Service Worker 的 PWA 配置：经 localhost 或 HTTPS 反代访问时可安装到主屏
并离线缓存应用外壳；局域网 HTTP 不是安全上下文，浏览器会跳过 SW 注册，页面功能不受
影响。页面仅提供：

- 自动发现状态和六位码兜底配对；
- 上游连接状态、重新连接与解除配对；
- 短信、蜂窝代理和 eSIM 三项结果状态；
- 本地管理凭据修改和设置页的运行信息。

源码在 webui/，构建产物提交在 app/src/main/assets/web/。修改网页后重新构建：

~~~bash
./scripts/build-webui.sh
~~~

除静态登录页和 POST /api/auth/login 外，所有 /api/* 均要求有效会话；POST、PUT、
DELETE 还要求 X-CSRF-Token。管理接口不启用 CORS，并返回 CSP、禁止嵌入和禁止 MIME
嗅探等安全响应头。

## 真机测试

无短信发送或删除的冒烟测试：

~~~bash
ADB="$ANDROID_HOME/platform-tools/adb" ./scripts/adb-smoke-test.sh
~~~

完整联调会临时启动 VoHive，验证网页鉴权、配对、状态/订阅/短信 RPC 和蜂窝代理：

~~~bash
ADB="$ANDROID_HOME/platform-tools/adb" ./scripts/adb-e2e-test.sh
~~~

## 权限层级

运行时权限不能由无屏网页触发 Android 权限弹窗，因此由 ADB、Device Owner 或系统镜像在
部署阶段授予。普通侧载安装可使用短信、状态和活动订阅能力；完整硬件标识符、无交互 eSIM
切换、射频控制和设备重启需要默认短信角色、运营商特权、Device Owner 或系统特权。

系统镜像部署时，将平台签名 APK 放入 priv-app/VoHiveAgent/，并安装：

- system/privapp-permissions-com.vohive.agent.xml → etc/permissions/
- system/default-permissions-com.vohive.agent.xml → etc/default-permissions/

ComposeSmsActivity 仅作为 Android 默认短信角色所需的 NoDisplay 合约组件存在；Agent 不含
可见的 Android 管理 UI。eSIM 需要用户确认时仍可能由系统显示平台确认界面。
