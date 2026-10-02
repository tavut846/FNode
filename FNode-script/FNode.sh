#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

# check root
[[ $EUID -ne 0 ]] && echo -e "${red}错误: ${plain} 必须使用root用户运行此脚本！\n" && exit 1

# check os
if [[ -f /etc/redhat-release ]]; then
    release="centos"
elif cat /etc/issue | grep -Eqi "alpine"; then
    release="alpine"
elif cat /etc/issue | grep -Eqi "debian"; then
    release="debian"
elif cat /etc/issue | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /etc/issue | grep -Eqi "centos|red hat|redhat|rocky|alma|oracle linux"; then
    release="centos"
elif cat /proc/version | grep -Eqi "debian"; then
    release="debian"
elif cat /proc/version | grep -Eqi "ubuntu"; then
    release="ubuntu"
elif cat /proc/version | grep -Eqi "centos|red hat|redhat|rocky|alma|oracle linux"; then
    release="centos"
elif cat /proc/version | grep -Eqi "arch"; then
    release="arch"
else
    echo -e "${red}未检测到系统版本，请联系脚本作者！${plain}\n" && exit 1
fi

# os version
if [[ -f /etc/os-release ]]; then
    os_version=$(awk -F'[= ."]' '/VERSION_ID/{print $3}' /etc/os-release)
fi
if [[ -z "$os_version" && -f /etc/lsb-release ]]; then
    os_version=$(awk -F'[= ."]+' '/DISTRIB_RELEASE/{print $2}' /etc/lsb-release)
fi

if [[ x"${release}" == x"centos" ]]; then
    if [[ ${os_version} -le 6 ]]; then
        echo -e "${red}请使用 CentOS 7 或更高版本的系统！${plain}\n" && exit 1
    fi
    if [[ ${os_version} -eq 7 ]]; then
        echo -e "${red}注意： CentOS 7 无法使用hysteria1/2协议！${plain}\n"
    fi
elif [[ x"${release}" == x"ubuntu" ]]; then
    if [[ ${os_version} -lt 16 ]]; then
        echo -e "${red}请使用 Ubuntu 16 或更高版本的系统！${plain}\n" && exit 1
    fi
elif [[ x"${release}" == x"debian" ]]; then
    if [[ ${os_version} -lt 8 ]]; then
        echo -e "${red}请使用 Debian 8 或更高版本的系统！${plain}\n" && exit 1
    fi
fi

# 检查系统是否有有效公网 IPv6 地址与路由
check_ipv6_support() {
    local has_global_ipv6=0
    local has_ipv6_route=0

    # 检查是否存在全局非保留 IPv6 地址 (排除 ::1 本地回环和 fe80:: 链路本地地址)
    if ip -6 addr show scope global 2>/dev/null | grep -q "inet6"; then
        has_global_ipv6=1
    fi

    # 检查是否存在 IPv6 默认路由网关
    if ip -6 route show default 2>/dev/null | grep -q "default"; then
        has_ipv6_route=1
    fi

    if [[ $has_global_ipv6 -eq 1 && $has_ipv6_route -eq 1 ]]; then
        echo "1"  # 支持并拥有可用公网 IPv6 (双栈网络)
    else
        echo "0"  # 纯 IPv4 (无有效公网 IPv6)
    fi
}

confirm() {
    if [[ $# > 1 ]]; then
        echo && read -rp "$1 [默认$2]: " temp
        if [[ x"${temp}" == x"" ]]; then
            temp=$2
        fi
    else
        read -rp "$1 [y/n]: " temp
    fi
    if [[ x"${temp}" == x"y" || x"${temp}" == x"Y" ]]; then
        return 0
    else
        return 1
    fi
}

confirm_restart() {
    confirm "是否重启FNode" "y"
    if [[ $? == 0 ]]; then
        restart
    else
        show_menu
    fi
}

before_show_menu() {
    echo && echo -n -e "${yellow}按回车返回主菜单: ${plain}" && read temp
    show_menu
}

install() {
    bash <(curl -Ls https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/install.sh)
}

update() {
    if [[ $# == 0 ]]; then
        echo && echo -n -e "输入指定版本(默认最新版): " && read version
    else
        version=$2
    fi
    bash <(curl -Ls https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/install.sh) $version
    if [[ $? == 0 ]]; then
        echo -e "${green}更新完成，已自动重启 FNode，请使用 FNode log 查看运行日志${plain}"
        exit
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

config() {
    echo "FNode在修改配置后会自动尝试重启"
    vi /etc/FNode/config.json
    sleep 2
    restart
    check_status
    case $? in
        0)
            echo -e "FNode状态: ${green}已运行${plain}"
            ;;
        1)
            echo -e "检测到您未启动FNode或FNode自动重启失败，是否查看日志？[Y/n]" && echo
            read -e -rp "(默认: y):" yn
            [[ -z ${yn} ]] && yn="y"
            if [[ ${yn} == [Yy] ]]; then
               show_log
            fi
            ;;
        2)
            echo -e "FNode状态: ${red}未安装${plain}"
    esac
}

uninstall() {
    confirm "确定要卸载 FNode 吗?" "n"
    if [[ $? != 0 ]]; then
        if [[ $# == 0 ]]; then
            show_menu
        fi
        return 0
    fi
    if [[ x"${release}" == x"alpine" ]]; then
        service FNode stop
        rc-update del FNode
        rm /etc/init.d/FNode -f
    else
        systemctl stop FNode
        systemctl disable FNode
        rm /etc/systemd/system/FNode.service -f
        systemctl daemon-reload
        systemctl reset-failed
    fi
    rm /etc/FNode/ -rf
    rm /usr/local/FNode/ -rf

    echo ""
    echo -e "卸载成功，如果你想删除此脚本，则退出脚本后运行 ${green}rm /usr/bin/FNode -f${plain} 进行删除"
    echo ""

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

start() {
    check_status
    if [[ $? == 0 ]]; then
        echo ""
        echo -e "${green}FNode已运行，无需再次启动，如需重启请选择重启${plain}"
    else
        if [[ x"${release}" == x"alpine" ]]; then
            service FNode start
        else
            systemctl start FNode
        fi
        sleep 2
        check_status
        if [[ $? == 0 ]]; then
            echo -e "${green}FNode 启动成功，请使用 FNode log 查看运行日志${plain}"
        else
            echo -e "${red}FNode可能启动失败，请稍后使用 FNode log 查看日志信息${plain}"
        fi
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

stop() {
    if [[ x"${release}" == x"alpine" ]]; then
        service FNode stop
    else
        systemctl stop FNode
    fi
    sleep 2
    check_status
    if [[ $? == 1 ]]; then
        echo -e "${green}FNode 停止成功${plain}"
    else
        echo -e "${red}FNode停止失败，可能是因为停止时间超过了两秒，请稍后查看日志信息${plain}"
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

restart() {
    if [[ x"${release}" == x"alpine" ]]; then
        service FNode restart
    else
        systemctl restart FNode
    fi
    sleep 2
    check_status
    if [[ $? == 0 ]]; then
        echo -e "${green}FNode 重启成功，请使用 FNode log 查看运行日志${plain}"
    else
        echo -e "${red}FNode可能启动失败，请稍后使用 FNode log 查看日志信息${plain}"
    fi
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

status() {
    if [[ x"${release}" == x"alpine" ]]; then
        service FNode status
    else
        systemctl status FNode --no-pager -l
    fi
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

enable() {
    if [[ x"${release}" == x"alpine" ]]; then
        rc-update add FNode
    else
        systemctl enable FNode
    fi
    if [[ $? == 0 ]]; then
        echo -e "${green}FNode 设置开机自启成功${plain}"
    else
        echo -e "${red}FNode 设置开机自启失败${plain}"
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

disable() {
    if [[ x"${release}" == x"alpine" ]]; then
        rc-update del FNode
    else
        systemctl disable FNode
    fi
    if [[ $? == 0 ]]; then
        echo -e "${green}FNode 取消开机自启成功${plain}"
    else
        echo -e "${red}FNode 取消开机自启失败${plain}"
    fi

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_log() {
    if [[ x"${release}" == x"alpine" ]]; then
        echo -e "${red}alpine系统暂不支持日志查看${plain}\n" && exit 1
    else
        journalctl -u FNode.service -e --no-pager -f
    fi
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

clean_log() {
    echo -e "${yellow}正在清理 FNode 运行日志...${plain}"

    # 1. 清理 systemd journal 日志 (针对 FNode 与 Caddy)
    if [[ x"${release}" != x"alpine" ]] && command -v journalctl &>/dev/null; then
        echo -e "正在清理 systemd 日志 (FNode.service)..."
        journalctl --rotate &>/dev/null
        journalctl --vacuum-time=1s --unit=FNode.service &>/dev/null
        if systemctl list-unit-files 2>/dev/null | grep -q "caddy.service"; then
            journalctl --vacuum-time=1s --unit=caddy.service &>/dev/null
        fi
        journalctl --vacuum-size=20M &>/dev/null
    fi

    # 2. 清理配置文件中指定的日志文件 (Log.Output 及 Cores[].Log.Output)
    config_file="/etc/FNode/config.json"
    if [[ -f "${config_file}" ]]; then
        log_paths=$(grep -E '"Output":\s*"[^"]+"' "${config_file}" 2>/dev/null | sed -E 's/.*"Output":\s*"([^"]+)".*/\1/' | grep -v '^$')
        for lp in ${log_paths}; do
            if [[ -f "${lp}" ]]; then
                echo -e "正在截断日志文件: ${lp}"
                truncate -s 0 "${lp}" 2>/dev/null || : > "${lp}"
            fi
        done
    fi

    # 3. 清理常见的独立日志文件
    common_logs=(
        "/var/log/FNode.log"
        "/var/log/fnode.log"
        "/var/log/fnode.error.log"
        "/usr/local/FNode/box.log"
        "/usr/local/FNode/fnode.log"
        "/etc/FNode/box.log"
        "/etc/FNode/fnode.log"
        "/var/log/caddy/caddy.log"
        "/var/log/caddy/access.log"
    )

    for log_file in "${common_logs[@]}"; do
        if [[ -f "${log_file}" ]]; then
            echo -e "正在截断日志文件: ${log_file}"
            truncate -s 0 "${log_file}" 2>/dev/null || : > "${log_file}"
        fi
    done

    echo -e "${green}FNode 日志清理完成！${plain}"
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

install_bbr() {
    bash <(curl -L -s https://github.com/ylx2016/Linux-NetSpeed/raw/master/tcpx.sh)
}

update_shell() {
    wget -O /usr/bin/FNode -N --no-check-certificate https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/FNode.sh
    if [[ $? != 0 ]]; then
        echo ""
        echo -e "${red}下载脚本失败，请检查本机能否连接 Github${plain}"
        before_show_menu
    else
        chmod +x /usr/bin/FNode
        echo -e "${green}升级脚本成功，请重新运行脚本${plain}" && exit 0
    fi
}

# 0: running, 1: not running, 2: not installed
check_status() {
    if [[ ! -f /usr/local/FNode/FNode ]]; then
        return 2
    fi
    if [[ x"${release}" == x"alpine" ]]; then
        temp=$(service FNode status | awk '{print $3}')
        if [[ x"${temp}" == x"started" ]]; then
            return 0
        else
            return 1
        fi
    else
        temp=$(systemctl status FNode | grep Active | awk '{print $3}' | cut -d "(" -f2 | cut -d ")" -f1)
        if [[ x"${temp}" == x"running" ]]; then
            return 0
        else
            return 1
        fi
    fi
}

check_enabled() {
    if [[ x"${release}" == x"alpine" ]]; then
        temp=$(rc-update show | grep FNode)
        if [[ x"${temp}" == x"" ]]; then
            return 1
        else
            return 0
        fi
    else
        temp=$(systemctl is-enabled FNode)
        if [[ x"${temp}" == x"enabled" ]]; then
            return 0
        else
            return 1;
        fi
    fi
}

check_uninstall() {
    check_status
    if [[ $? != 2 ]]; then
        echo ""
        echo -e "${red}FNode已安装，请不要重复安装${plain}"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

check_install() {
    check_status
    if [[ $? == 2 ]]; then
        echo ""
        echo -e "${red}请先安装FNode${plain}"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

show_status() {
    check_status
    case $? in
        0)
            echo -e "FNode状态: ${green}已运行${plain}"
            show_enable_status
            ;;
        1)
            echo -e "FNode状态: ${yellow}未运行${plain}"
            show_enable_status
            ;;
        2)
            echo -e "FNode状态: ${red}未安装${plain}"
    esac
}

show_enable_status() {
    check_enabled
    if [[ $? == 0 ]]; then
        echo -e "是否开机自启: ${green}是${plain}"
    else
        echo -e "是否开机自启: ${red}否${plain}"
    fi
}

generate_x25519_key() {
    echo -n "正在生成 x25519 密钥："
    /usr/local/FNode/FNode x25519
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_FNode_version() {
    echo -n "FNode 版本："
    /usr/local/FNode/FNode version
    echo ""
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

add_node_config() {
    echo -e "${green}节点核心类型：singbox${plain}"
    core_type="1"
    core="sing"
    core_sing=true
    while true; do
        read -rp "请输入节点Node ID：" NodeID
        # 判断NodeID是否为正整数
        if [[ "$NodeID" =~ ^[0-9]+$ ]]; then
            break
        else
            echo "错误：请输入正确的数字作为Node ID。"
        fi
    done

    echo -e "${yellow}请选择节点传输协议：${plain}"
    echo -e "${green}1. Shadowsocks${plain}"
    echo -e "${green}2. Vless${plain}"
    echo -e "${green}3. Vmess${plain}"
    echo -e "${green}4. Hysteria${plain}"
    echo -e "${green}5. Hysteria2${plain}"
    echo -e "${green}6. Trojan${plain}"  
    echo -e "${green}7. Tuic${plain}"
    echo -e "${green}8. AnyTLS${plain}"
    read -rp "请输入：" NodeType
    case "$NodeType" in
        1 ) NodeType="shadowsocks" ;;
        2 ) NodeType="vless" ;;
        3 ) NodeType="vmess" ;;
        4 ) NodeType="hysteria" ;;
        5 ) NodeType="hysteria2" ;;
        6 ) NodeType="trojan" ;;
        7 ) NodeType="tuic" ;;
        8 ) NodeType="anytls" ;;
        * ) NodeType="shadowsocks" ;;
    esac
    fastopen=true
    if [ "$NodeType" == "vless" ]; then
        read -rp "请选择是否为reality节点？(y/n)" isreality
    elif [ "$NodeType" == "hysteria" ] || [ "$NodeType" == "hysteria2" ] || [ "$NodeType" == "tuic" ] || [ "$NodeType" == "anytls" ]; then
        fastopen=false
        istls="y"
    fi

    if [[ "$isreality" != "y" && "$isreality" != "Y" &&  "$istls" != "y" ]]; then
        read -rp "请选择是否进行TLS配置？(y/n)" istls
    fi

    certmode="none"
    certdomain="example.com"
    certfile="/etc/FNode/fullchain.cer"
    keyfile="/etc/FNode/cert.key"
    if [[ "$isreality" != "y" && "$isreality" != "Y" && ( "$istls" == "y" || "$istls" == "Y" ) ]]; then
        echo -e "${yellow}请选择证书申请模式：${plain}"
        echo -e "${green}1. http模式自动申请，节点域名已正确解析${plain}"
        echo -e "${green}2. dns模式自动申请，需填入正确域名服务商API参数${plain}"
        echo -e "${green}3. self模式，自签证书${plain}"
        echo -e "${green}4. file模式，使用已有证书文件 (支持自动检测 Caddy 证书)${plain}"
        read -rp "请输入：" certmode
        case "$certmode" in
            1 ) certmode="http" ;;
            2 ) certmode="dns" ;;
            3 ) certmode="self" ;;
            4 ) certmode="file" ;;
            * ) certmode="none" ;;
        esac

        caddy_base_dir="/root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory"
        if [ "$certmode" == "file" ]; then
            if [ -d "$caddy_base_dir" ]; then
                echo -e "${green}检测到 Caddy 证书目录存在，发现以下已申请证书的域名：${plain}"
                ls -1 "$caddy_base_dir" 2>/dev/null
            fi
            read -rp "请输入节点证书域名 (例如: domain.com)：" certdomain
            caddy_cert="${caddy_base_dir}/${certdomain}/${certdomain}.crt"
            caddy_key="${caddy_base_dir}/${certdomain}/${certdomain}.key"
            if [ -f "$caddy_cert" ] && [ -f "$caddy_key" ]; then
                echo -e "${green}已成功检测并匹配到 Caddy 证书与私钥！${plain}"
                echo -e "${green}CertFile: ${caddy_cert}${plain}"
                echo -e "${green}KeyFile:  ${caddy_key}${plain}"
                certfile="$caddy_cert"
                keyfile="$caddy_key"
            else
                echo -e "${yellow}未在标准 Caddy 目录下检测到该域名的证书文件，请手动输入路径或使用默认路径：${plain}"
                read -rp "请输入证书文件路径 (默认: ${caddy_cert}): " input_certfile
                read -rp "请输入私钥文件路径 (默认: ${caddy_key}): " input_keyfile
                certfile="${input_certfile:-$caddy_cert}"
                keyfile="${input_keyfile:-$caddy_key}"
            fi
        else
            read -rp "请输入节点证书域名(example.com)：" certdomain
            if [ "$certmode" != "http" ]; then
                echo -e "${red}请手动修改配置文件后重启FNode！${plain}"
            fi
        fi
    fi
    ipv6_support=$(check_ipv6_support)
    listen_ip="0.0.0.0"
    disable_ipv6_node="true"
    if [ "$ipv6_support" -eq 1 ]; then
        listen_ip="::"
    fi
    node_config=""
    node_config=$(cat <<EOF
{
            "Core": "$core",
            "ApiHost": "$ApiHost",
            "ApiKey": "$ApiKey",
            "NodeID": $NodeID,
            "NodeType": "$NodeType",
            "Timeout": 30,
            "ListenIP": "$listen_ip",
            "SendIP": "0.0.0.0",
            "DisableIPv6": $disable_ipv6_node,
            "DeviceOnlineMinTraffic": 200,
            "MinReportTraffic": 0,
            "TCPFastOpen": $fastopen,
            "SniffEnabled": true,
            "Masquerade": "$masquerade_target",
            "CertConfig": {
                "CertMode": "$certmode",
                "RejectUnknownSni": false,
                "CertDomain": "$certdomain",
                "CertFile": "$certfile",
                "KeyFile": "$keyfile",
                "Email": "fnode@github.com",
                "Provider": "cloudflare",
                "DNSEnv": {
                    "EnvName": "env1"
                }
            }
        },
EOF
)
    nodes_config+=("$node_config")
}

generate_config_file() {
    echo -e "${yellow}FNode 配置文件生成向导${plain}"
    echo -e "${red}请阅读以下注意事项：${plain}"
    echo -e "${red}1. 目前该功能正处测试阶段${plain}"
    echo -e "${red}2. 生成的配置文件会保存到 /etc/FNode/config.json${plain}"
    echo -e "${red}3. 原来的配置文件会保存到 /etc/FNode/config.json.bak${plain}"
    echo -e "${red}4. 目前仅部分支持TLS${plain}"
    read -rp "是否继续？(y/n): " continue_prompt
    if [[ "$continue_prompt" =~ ^[Nn][Oo]? ]]; then
        exit 0
    fi

    # 检测 VPS 网络环境 (IPv4 与 IPv6)
    ipv6_support=$(check_ipv6_support)
    if [ "$ipv6_support" -eq 1 ]; then
        echo -e "${green}[网络环境检测] 当前 VPS 具备 IPv4 + IPv6 双栈网络，入站已绑定 [::] 双栈。${plain}"
        echo -e "${green}默认启用 DisableIPv6: true（出站优先/独占 IPv4，彻底杜绝 IPv6 路由绕路与 AAAA DNS 超时）。${plain}"
        disable_ipv6_core="true"
        domain_strategy="prefer_ipv4"
    else
        echo -e "${yellow}[网络环境检测] 当前 VPS 仅具备 IPv4 网络 (未检测到有效公网 IPv6)。${plain}"
        echo -e "${green}已自动配置: DisableIPv6: true，防止客户端本地 IPv6 请求导致连接超时。${plain}"
        disable_ipv6_core="true"
        domain_strategy="prefer_ipv4"
    fi
    
    nodes_config=()
    first_node=true
    core_xray=false
    core_sing=false
    fixed_api_info=false
    check_api=false
    
    while true; do
        if [ "$first_node" = true ]; then
            read -rp "请输入机场网址(https://example.com)：" ApiHost
            read -rp "请输入面板对接API Key：" ApiKey
            read -rp "是否设置固定的机场网址和API Key？(y/n)" fixed_api
            if [ "$fixed_api" = "y" ] || [ "$fixed_api" = "Y" ]; then
                fixed_api_info=true
                echo -e "${red}成功固定地址${plain}"
            fi
            first_node=false
            add_node_config
        else
            read -rp "是否继续添加节点配置？(回车继续，输入n或no退出)" continue_adding_node
            if [[ "$continue_adding_node" =~ ^[Nn][Oo]? ]]; then
                break
            elif [ "$fixed_api_info" = false ]; then
                read -rp "请输入机场网址：" ApiHost
                read -rp "请输入面板对接API Key：" ApiKey
            fi
            add_node_config
        fi
    done

    # 初始化核心配置数组
    cores_config="[
    {
        \"Type\": \"sing\",
        \"Log\": {
            \"Level\": \"error\",
            \"Timestamp\": true
        },
        \"DisableIPv6\": ${disable_ipv6_core},
        \"DomainStrategy\": \"${domain_strategy}\",
        \"NTP\": {
            \"Enable\": true,
            \"Server\": \"time.apple.com\",
            \"ServerPort\": 0
        },
        \"OriginalPath\": \"/etc/FNode/sing_origin.json\"
    }]"

    # 切换到配置文件目录
    cd /etc/FNode
    
    # 备份旧的配置文件
    mv config.json config.json.bak
    nodes_config_str="${nodes_config[*]}"
    formatted_nodes_config="${nodes_config_str%,}"

    # 创建 config.json 文件
    cat <<EOF > /etc/FNode/config.json
{
    "Log": {
        "Level": "error",
        "Output": ""
    },
    "Cores": $cores_config,
    "Nodes": [$formatted_nodes_config]
}
EOF
    
    # 创建 custom_outbound.json 文件
    cat <<EOF > /etc/FNode/custom_outbound.json
    [
        {
            "tag": "IPv4_out",
            "protocol": "freedom",
            "settings": {
                "domainStrategy": "UseIPv4v6"
            }
        },
        {
            "tag": "IPv6_out",
            "protocol": "freedom",
            "settings": {
                "domainStrategy": "UseIPv6"
            }
        },
        {
            "protocol": "blackhole",
            "tag": "block"
        }
    ]
EOF
    
    # 创建 route.json 文件
    cat <<EOF > /etc/FNode/route.json
    {
        "domainStrategy": "AsIs",
        "rules": [
            {
                "type": "field",
                "outboundTag": "block",
                "ip": [
                    "geoip:private"
                ]
            },
            {
                "type": "field",
                "outboundTag": "block",
                "domain": [
                    "regexp:(api|ps|sv|offnavi|newvector|ulog.imap|newloc)(.map|).(baidu|n.shifen).com",
                    "regexp:(.+.|^)(360|so).(cn|com)",
                    "regexp:(Subject|HELO|SMTP)",
                    "regexp:(torrent|.torrent|peer_id=|info_hash|get_peers|find_node|BitTorrent|announce_peer|announce.php?passkey=)",
                    "regexp:(^.@)(guerrillamail|guerrillamailblock|sharklasers|grr|pokemail|spam4|bccto|chacuo|027168).(info|biz|com|de|net|org|me|la)",
                    "regexp:(.?)(xunlei|sandai|Thunder|XLLiveUD)(.)",
                    "regexp:(..||)(dafahao|mingjinglive|botanwang|minghui|dongtaiwang|falunaz|epochtimes|ntdtv|falundafa|falungong|wujieliulan|zhengjian).(org|com|net)",
                    "regexp:(ed2k|.torrent|peer_id=|announce|info_hash|get_peers|find_node|BitTorrent|announce_peer|announce.php?passkey=|magnet:|xunlei|sandai|Thunder|XLLiveUD|bt_key)",
                    "regexp:(.+.|^)(360).(cn|com|net)",
                    "regexp:(.*.||)(guanjia.qq.com|qqpcmgr|QQPCMGR)",
                    "regexp:(.*.||)(rising|kingsoft|duba|xindubawukong|jinshanduba).(com|net|org)",
                    "regexp:(.*.||)(netvigator|torproject).(com|cn|net|org)",
                    "regexp:(..||)(visa|mycard|gash|beanfun|bank).",
                    "regexp:(.*.||)(gov|12377|12315|talk.news.pts.org|creaders|zhuichaguoji|efcc.org|cyberpolice|aboluowang|tuidang|epochtimes|zhengjian|110.qq|mingjingnews|inmediahk|xinsheng|breakgfw|chengmingmag|jinpianwang|qi-gong|mhradio|edoors|renminbao|soundofhope|xizang-zhiye|bannedbook|ntdtv|12321|secretchina|dajiyuan|boxun|chinadigitaltimes|dwnews|huaglad|oneplusnews|epochweekly|cn.rfi).(cn|com|org|net|club|net|fr|tw|hk|eu|info|me)",
                    "regexp:(.*.||)(miaozhen|cnzz|talkingdata|umeng).(cn|com)",
                    "regexp:(.*.||)(mycard).(com|tw)",
                    "regexp:(.*.||)(gash).(com|tw)",
                    "regexp:(.bank.)",
                    "regexp:(.*.||)(pincong).(rocks)",
                    "regexp:(.*.||)(taobao).(com)",
                    "regexp:(.*.||)(laomoe|jiyou|ssss|lolicp|vv1234|0z|4321q|868123|ksweb|mm126).(com|cloud|fun|cn|gs|xyz|cc)",
                    "regexp:(flows|miaoko).(pages).(dev)"
                ]
            },
            {
                "type": "field",
                "outboundTag": "block",
                "ip": [
                    "127.0.0.1/32",
                    "10.0.0.0/8",
                    "fc00::/7",
                    "fe80::/10",
                    "172.16.0.0/12"
                ]
            },
            {
                "type": "field",
                "outboundTag": "block",
                "protocol": [
                    "bittorrent"
                ]
            }
        ]
    }
EOF

    ipv6_support=$(check_ipv6_support)
    dnsstrategy="ipv4_only"
    if [ "$ipv6_support" -eq 1 ]; then
        dnsstrategy="prefer_ipv4"
    fi
    # 创建 sing_origin.json 文件
    cat <<EOF > /etc/FNode/sing_origin.json
{
  "dns": {
    "servers": [
      {
        "tag": "cf",
        "address": "1.1.1.1"
      }
    ],
    "strategy": "$dnsstrategy"
  },
  "outbounds": [
    {
      "tag": "direct",
      "type": "direct",
      "domain_resolver": {
        "server": "cf",
        "strategy": "$dnsstrategy"
      }
    },
    {
      "type": "block",
      "tag": "block"
    }
  ],
  "route": {
    "rules": [
      {
        "ip_is_private": true,
        "outbound": "block"
      },
      {
        "domain_regex": [
            "(api|ps|sv|offnavi|newvector|ulog.imap|newloc)(.map|).(baidu|n.shifen).com",
            "(.+.|^)(360|so).(cn|com)",
            "(Subject|HELO|SMTP)",
            "(torrent|.torrent|peer_id=|info_hash|get_peers|find_node|BitTorrent|announce_peer|announce.php?passkey=)",
            "(^.@)(guerrillamail|guerrillamailblock|sharklasers|grr|pokemail|spam4|bccto|chacuo|027168).(info|biz|com|de|net|org|me|la)",
            "(.?)(xunlei|sandai|Thunder|XLLiveUD)(.)",
            "(..||)(dafahao|mingjinglive|botanwang|minghui|dongtaiwang|falunaz|epochtimes|ntdtv|falundafa|falungong|wujieliulan|zhengjian).(org|com|net)",
            "(ed2k|.torrent|peer_id=|announce|info_hash|get_peers|find_node|BitTorrent|announce_peer|announce.php?passkey=|magnet:|xunlei|sandai|Thunder|XLLiveUD|bt_key)",
            "(.+.|^)(360).(cn|com|net)",
            "(.*.||)(guanjia.qq.com|qqpcmgr|QQPCMGR)",
            "(.*.||)(rising|kingsoft|duba|xindubawukong|jinshanduba).(com|net|org)",
            "(.*.||)(netvigator|torproject).(com|cn|net|org)",
            "(..||)(visa|mycard|gash|beanfun|bank).",
            "(.*.||)(gov|12377|12315|talk.news.pts.org|creaders|zhuichaguoji|efcc.org|cyberpolice|aboluowang|tuidang|epochtimes|zhengjian|110.qq|mingjingnews|inmediahk|xinsheng|breakgfw|chengmingmag|jinpianwang|qi-gong|mhradio|edoors|renminbao|soundofhope|xizang-zhiye|bannedbook|ntdtv|12321|secretchina|dajiyuan|boxun|chinadigitaltimes|dwnews|huaglad|oneplusnews|epochweekly|cn.rfi).(cn|com|org|net|club|net|fr|tw|hk|eu|info|me)",
            "(.*.||)(miaozhen|cnzz|talkingdata|umeng).(cn|com)",
            "(.*.||)(mycard).(com|tw)",
            "(.*.||)(gash).(com|tw)",
            "(.bank.)",
            "(.*.||)(pincong).(rocks)",
            "(.*.||)(taobao).(com)",
            "(.*.||)(laomoe|jiyou|ssss|lolicp|vv1234|0z|4321q|868123|ksweb|mm126).(com|cloud|fun|cn|gs|xyz|cc)",
            "(flows|miaoko).(pages).(dev)"
        ],
        "outbound": "block"
      },
      {
        "outbound": "direct",
        "network": [
          "udp","tcp"
        ]
      }
    ]
  },
  "experimental": {
    "cache_file": {
      "enabled": true
    }
  }
}
EOF

    echo -e "${green}FNode 配置文件生成完成，正在重新启动 FNode 服务${plain}"
    restart 0
    before_show_menu
}

# 放开防火墙端口
open_ports() {
    systemctl stop firewalld.service 2>/dev/null
    systemctl disable firewalld.service 2>/dev/null
    setenforce 0 2>/dev/null
    ufw disable 2>/dev/null
    iptables -P INPUT ACCEPT 2>/dev/null
    iptables -P FORWARD ACCEPT 2>/dev/null
    iptables -P OUTPUT ACCEPT 2>/dev/null
    iptables -t nat -F 2>/dev/null
    iptables -t mangle -F 2>/dev/null
    iptables -F 2>/dev/null
    iptables -X 2>/dev/null
    netfilter-persistent save 2>/dev/null
    echo -e "${green}放开防火墙端口成功！${plain}"
}

# 安装 Caddy (集成 Cloudflare DNS 模块)
install_caddy() {
    echo -e "${green}开始安装 Caddy (包含 Cloudflare DNS 模块)...${plain}"
    caddy_arch="amd64"
    if [[ $arch == "arm64-v8a" || $arch == "arm64" || $(uname -m) == "aarch64" ]]; then
        caddy_arch="arm64"
    elif [[ $arch == "s390x" ]]; then
        caddy_arch="s390x"
    else
        caddy_arch="amd64"
    fi

    mkdir -p /etc/caddy
    echo -e "正在从官方获取集成 Cloudflare DNS 模块的 Caddy 二进制文件..."
    curl -o /usr/bin/caddy -L "https://caddyserver.com/api/download?os=linux&arch=${caddy_arch}&p=github.com%2Fcaddy-dns%2Fcloudflare"
    if [[ $? -ne 0 || ! -s /usr/bin/caddy ]]; then
        echo -e "${red}下载带 Cloudflare 模块的 Caddy 失败，尝试标准 Caddy 安装...${plain}"
        if [[ x"${release}" == x"debian" || x"${release}" == x"ubuntu" ]]; then
            apt update -y >/dev/null 2>&1
            apt install -y debian-keyring debian-archive-keyring apt-transport-https curl >/dev/null 2>&1
            curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg --yes >/dev/null 2>&1
            curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null 2>&1
            apt update -y >/dev/null 2>&1
            apt install -y caddy >/dev/null 2>&1
        elif [[ x"${release}" == x"centos" ]]; then
            yum install -y yum-plugin-copr >/dev/null 2>&1
            yum copr enable -y @caddy/caddy >/dev/null 2>&1
            yum install -y caddy >/dev/null 2>&1
        fi
    else
        chmod +x /usr/bin/caddy
    fi

    if [[ x"${release}" == x"alpine" ]]; then
        cat <<EOF > /etc/init.d/caddy
#!/sbin/openrc-run

name="caddy"
description="Caddy web server"

command="/usr/bin/caddy"
command_args="run --config /etc/caddy/Caddyfile"
command_user="root"
pidfile="/run/caddy.pid"
command_background="yes"

depend() {
    need net
}
EOF
        chmod +x /etc/init.d/caddy
        rc-update add caddy default >/dev/null 2>&1
    else
        cat <<EOF > /etc/systemd/system/caddy.service
[Unit]
Description=Caddy
Documentation=https://caddyserver.com/docs/
After=network.target network-online.target
Requires=network-online.target

[Service]
Type=notify
User=root
Group=root
EnvironmentFile=-/etc/caddy/caddy.env
ExecStart=/usr/bin/caddy run --environ --config /etc/caddy/Caddyfile
ExecReload=/usr/bin/caddy reload --config /etc/caddy/Caddyfile --force
TimeoutStopSec=5s
LimitNOFILE=1048576
PrivateTmp=true
ProtectSystem=full
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF
        systemctl daemon-reload
        systemctl enable caddy >/dev/null 2>&1
    fi
    echo -e "${green}Caddy 安装并配置服务完成！${plain}"
}

# 配置 Caddy 反向代理与证书申请 (支持多域名)
setup_caddy_reverse_proxy() {
    if ! command -v caddy &>/dev/null; then
        echo -e "${yellow}未检测到 Caddy，正在为您自动安装 Caddy...${plain}"
        install_caddy
    fi

    echo -e "${yellow}===== Caddy 反向代理与 Cloudflare DNS 证书配置向导 =====${plain}"

    # 自动探测 /etc/FNode/config.json 中已配置的 CertDomain
    detected_domains=""
    if [[ -f "/etc/FNode/config.json" ]]; then
        detected_domains=$(grep -E '"CertDomain":\s*"[^"]+"' /etc/FNode/config.json 2>/dev/null | sed -E 's/.*"CertDomain":\s*"([^"]+)".*/\1/' | tr '\n' ' ' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')
    fi
    if [[ -n "$detected_domains" ]]; then
        echo -e "${green}检测到 FNode 配置文件 (config.json) 中的域名: ${detected_domains}${plain}"
    fi

    echo -e "请输入需要配置的域名 (支持多个域名，用逗号或空格分隔，例如: domain1.com, domain2.com)"
    read -rp "域名 [默认: ${detected_domains:-domain.com}]: " input_domains
    caddy_domain_input="${input_domains:-$detected_domains}"
    if [ -z "$caddy_domain_input" ]; then
        echo -e "${red}域名不能为空！${plain}"
        if [[ $# == 0 ]]; then before_show_menu; fi
        return 1
    fi

    # 解析多个域名 (兼容逗号、分号、空格分隔)
    IFS=',; ' read -r -a domain_list <<< "$caddy_domain_input"
    valid_domains=()
    for d in "${domain_list[@]}"; do
        clean_d=$(echo "$d" | tr -d '"' | tr -d "'" | xargs)
        if [[ -n "$clean_d" ]]; then
            valid_domains+=("$clean_d")
        fi
    done

    if [[ ${#valid_domains[@]} -eq 0 ]]; then
        echo -e "${red}未能识别有效的域名输入！${plain}"
        if [[ $# == 0 ]]; then before_show_menu; fi
        return 1
    fi

    existing_token=""
    if [[ -f "/etc/caddy/caddy.env" ]]; then
        existing_token=$(grep -E '^CLOUDFLARE_API_TOKEN=' /etc/caddy/caddy.env 2>/dev/null | cut -d '=' -f2-)
    fi

    if [[ -n "$existing_token" ]]; then
        read -rp "请输入 Cloudflare API Token [直接回车使用已保存的 Token]: " cf_token
        cf_token="${cf_token:-$existing_token}"
    else
        read -rp "请输入 Cloudflare API Token (用于 DNS-01 验证申请证书): " cf_token
    fi

    if [ -z "$cf_token" ]; then
        echo -e "${red}Cloudflare API Token 不能为空！${plain}"
        if [[ $# == 0 ]]; then before_show_menu; fi
        return 1
    fi

    read -rp "请输入反向代理伪装目标网站 (默认: https://simulate-news.316293.xyz): " proxy_dest
    if [ -z "$proxy_dest" ]; then
        proxy_dest="https://simulate-news.316293.xyz"
    fi

    # 提取目标网站的 Host (如 simulate-news.316293.xyz)
    target_host=$(echo "$proxy_dest" | sed -e 's|^[^/]*//||' -e 's|/.*$||')

    mkdir -p /etc/caddy
    echo "CLOUDFLARE_API_TOKEN=${cf_token}" > /etc/caddy/caddy.env
    chmod 600 /etc/caddy/caddy.env

    caddyfile_content=""
    for dom in "${valid_domains[@]}"; do
        caddyfile_content+="${dom} {
    encode gzip

    tls {
        dns cloudflare {env.CLOUDFLARE_API_TOKEN}
        protocols tls1.2 tls1.3
    }

    reverse_proxy ${proxy_dest} {
        header_up Host ${target_host}
        header_up X-Real-IP {http.request.remote}
        header_up X-Forwarded-Proto https
        header_up Cache-Control \"no-cache, no-store, must-revalidate\"
        header_up Pragma \"no-cache\"
        header_up Expires \"0\"

        header_down Cache-Control \"no-cache, no-store, must-revalidate\"
        header_down Pragma \"no-cache\"
        header_down Expires \"0\"
    }
}

"
    done

    if [[ -f "/etc/caddy/Caddyfile" ]]; then
        echo -e "${yellow}检测到已存在 /etc/caddy/Caddyfile 配置文件：${plain}"
        echo -e "1. 覆盖旧配置 (推荐)"
        echo -e "2. 追加新域名到现有配置后"
        read -rp "请选择 [默认: 1]: " caddy_write_mode
        if [[ "$caddy_write_mode" == "2" ]]; then
            echo -e "\n${caddyfile_content}" >> /etc/caddy/Caddyfile
        else
            echo "${caddyfile_content}" > /etc/caddy/Caddyfile
        fi
    else
        echo "${caddyfile_content}" > /etc/caddy/Caddyfile
    fi

    echo -e "${green}正在启动 Caddy 服务并申请证书...${plain}"
    if [[ x"${release}" == x"alpine" ]]; then
        service caddy restart
    else
        systemctl restart caddy
        sleep 2
        systemctl status caddy --no-pager
    fi

    echo -e "${green}========================================${plain}"
    echo -e "${green}Caddy 多域名反向代理已成功配置！${plain}"
    echo -e "已配置域名列表:"
    for dom in "${valid_domains[@]}"; do
        echo -e "  - 域名: ${dom}"
        echo -e "    证书路径: /root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/${dom}/${dom}.crt"
        echo -e "    私钥路径: /root/.local/share/caddy/certificates/acme-v02.api.letsencrypt.org-directory/${dom}/${dom}.key"
    done
    echo -e "伪装反代目标: ${proxy_dest}"
    echo -e "${green}现在你可以运行 FNode 生成配置，并在各节点中选择 'file' 模式自动对接对应域名的证书！${plain}"
    echo -e "${green}========================================${plain}"
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

show_usage() {
    echo "FNode 管理脚本使用方法: "
    echo "------------------------------------------"
    echo "FNode              - 显示管理菜单 (功能更多)"
    echo "FNode start        - 启动 FNode"
    echo "FNode stop         - 停止 FNode"
    echo "FNode restart      - 重启 FNode"
    echo "FNode status       - 查看 FNode 状态"
    echo "FNode enable       - 设置 FNode 开机自启"
    echo "FNode disable      - 取消 FNode 开机自启"
    echo "FNode log          - 查看 FNode 日志"
    echo "FNode clearlog     - 清理 FNode 日志"
    echo "FNode x25519       - 生成 x25519 密钥"
    echo "FNode generate     - 生成 FNode 配置文件"
    echo "FNode update       - 更新 FNode"
    echo "FNode update x.x.x - 安装 FNode 指定版本"
    echo "FNode install      - 安装 FNode"
    echo "FNode uninstall    - 卸载 FNode"
    echo "FNode caddy        - 安装/配置 Caddy (支持 Cloudflare DNS 反代与证书)"
    echo "FNode version      - 查看 FNode 版本"
    echo "------------------------------------------"
}

show_menu() {
    fnode_version="未安装"
    if [[ -f /usr/local/FNode/FNode ]]; then
        fnode_version=$(/usr/local/FNode/FNode version --short 2>/dev/null || /usr/local/FNode/FNode version 2>/dev/null | grep "Version:" | awk '{print $2}')
    fi
    echo -e "
  ${green}FNode 后端管理脚本，${plain}${red}不适用于docker${plain}
  ${green}当前 FNode 版本: ${plain}${red}${fnode_version}${plain}
--- https://github.com/tavut846/FNode ---
  ${green}0.${plain} 修改配置
  ${green}1.${plain} 安装 FNode
  ${green}2.${plain} 更新 FNode
  ${green}3.${plain} 卸载 FNode
————————————————
  ${green}4.${plain} 启动 FNode
  ${green}5.${plain} 停止 FNode
  ${green}6.${plain} 重启 FNode
  ${green}7.${plain} 查看 FNode 状态
  ${green}8.${plain} 查看 FNode 日志
  ${green}9.${plain} 清理 FNode 日志
————————————————
  ${green}10.${plain} 设置 FNode 开机自启
  ${green}11.${plain} 取消 FNode 开机自启
————————————————
  ${green}12.${plain} 一键安装 bbr (最新内核)
  ${green}13.${plain} 查看 FNode 版本
  ${green}14.${plain} 生成 X25519 密钥
  ${green}15.${plain} 升级 FNode 维护脚本
  ${green}16.${plain} 生成 FNode 配置文件
  ${green}17.${plain} 放行 VPS 的所有网络端口
  ${green}18.${plain} 安装/配置 Caddy 反代与证书
  ${green}19.${plain} 退出脚本
 "
 #后续更新可加入上方字符串中
    show_status
    echo && read -rp "请输入选择 [0-19]: " num

    case "${num}" in
        0) config ;;
        1) check_uninstall && install ;;
        2) check_install && update ;;
        3) check_install && uninstall ;;
        4) check_install && start ;;
        5) check_install && stop ;;
        6) check_install && restart ;;
        7) check_install && status ;;
        8) check_install && show_log ;;
        9) check_install && clean_log ;;
        10) check_install && enable ;;
        11) check_install && disable ;;
        12) install_bbr ;;
        13) check_install && show_FNode_version ;;
        14) check_install && generate_x25519_key ;;
        15) update_shell ;;
        16) generate_config_file ;;
        17) open_ports ;;
        18) setup_caddy_reverse_proxy ;;
        19) exit ;;
        *) echo -e "${red}请输入正确的数字 [0-19]${plain}" ;;
    esac
}


if [[ $# > 0 ]]; then
    case $1 in
        "start") check_install 0 && start 0 ;;
        "stop") check_install 0 && stop 0 ;;
        "restart") check_install 0 && restart 0 ;;
        "status") check_install 0 && status 0 ;;
        "enable") check_install 0 && enable 0 ;;
        "disable") check_install 0 && disable 0 ;;
        "log") check_install 0 && show_log 0 ;;
        "clearlog"|"cleanlog"|"clean_log") check_install 0 && clean_log 0 ;;
        "update") check_install 0 && update 0 $2 ;;
        "config") config $* ;;
        "generate") generate_config_file ;;
        "install") check_uninstall 0 && install 0 ;;
        "uninstall") check_install 0 && uninstall 0 ;;
        "x25519") check_install 0 && generate_x25519_key 0 ;;
        "version") check_install 0 && show_FNode_version 0 ;;
        "caddy") setup_caddy_reverse_proxy 0 ;;
        "install_caddy") install_caddy 0 ;;
        "update_shell") update_shell ;;
        *) show_usage
    esac
else
    show_menu
fi
