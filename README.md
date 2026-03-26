# FNode

A V2board node server based on sing-box core, copied from V2bX.  
一个基于 sing-box 内核的V2board节点服务端，从 V2bX 复制而来，支持V2ay,Trojan,Shadowsocks,Hysteria2,Tuic等协议。

**注意： 本项目需要搭配[修改版V2board](https://github.com/wyx2685/v2board)**

## 特点

* 永久开源且免费。
* 支持多种协议 (Vmess/Vless, Trojan, Shadowsocks, Hysteria2, Tuic, AnyTLS).
* 支持Vless和reality等新特性。
* 支持单实例对接多节点，无需重复启动。
* 支持限制在线IP。
* 支持限制Tcp连接数。
* 支持节点端口级别、用户级别限速。
* 配置简单明了。
* 修改配置自动重启实例。
* 基于 sing-box 内核，性能卓越且功能齐全。

## 功能介绍

| 功能        | sing-box |
|-----------|-------|
| 自动申请tls证书 | √     |
| 自动续签tls证书 | √     |
| 在线人数统计    | √     |
| 审计规则      | √     |
| 自定义DNS    | √     |
| 在线IP数限制   | √     |
| 连接数限制     | √     |
| 按照用户限速    | √     |
| 动态限速(未测试) | √     |

## 软件安装

### 一键安装

```
wget -N https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/install.sh && bash install.sh
```

## 构建

# 编译 sing 内核

GOEXPERIMENT=jsonv2 go build -v -o build_assets/FNode -tags "sing with_quic with_grpc with_utls with_wireguard with_acme with_gvisor" -trimpath -ldflags "-X 'github.com/tavut846/FNode/cmd.version=$version' -s -w -buildid="

## 免责声明

* 本项目复制自 [V2bX](https://github.com/wyx2685/V2bX)，只是学术研究和个人使用。
* 本人不对使用本项目产生的任何 bug、错误或后果承担任何责任。
* 由于本人能力有限，无法保证所有功能的稳定性和可用性。

## Thanks

* [Project X](https://github.com/XTLS/)
* [V2Fly](https://github.com/v2fly)
* [VNet-V2ray](https://github.com/ProxyPanel/VNet-V2ray)
* [Air-Universe](https://github.com/crossfw/Air-Universe)
* [XrayR](https://github.com/XrayR/XrayR)
* [sing-box](https://github.com/SagerNet/sing-box)
