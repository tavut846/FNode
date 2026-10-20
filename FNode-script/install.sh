#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

cur_dir=$(pwd)
FNODE_BRANCH="${FNODE_BRANCH:-master}"

usage() {
    cat <<'USAGE'
Usage:
  install.sh [version]                                   interactive install (old behaviour)
  install.sh --mode machine --panel URL --token MACHINE_TOKEN --machine-id ID [options]
  install.sh --mode node    --panel URL --token SERVER_TOKEN  --node-id ID [--type TYPE] [options]
  install.sh uninstall [--yes]

Piped:  curl -fsSL https://raw.githubusercontent.com/tavut846/FNode/master/FNode-script/install.sh | sudo bash -s -- --mode machine --panel 'https://panel' --token 'MACHINE_TOKEN' --machine-id 3

Options:
  --version x.y.z|latest|pre   FNode release to install (default: latest)
  --branch NAME                branch used for the FNode.sh management script (default: master)
  --type TYPE                  node mode only: vless, vmess, trojan, shadowsocks, hysteria, hysteria2,
                               tuic, anytls. Omit it to let FNode read the type from the panel (v2 API)
  --cert-mode MODE             none (default), http, dns, self or file
  --cert-domain DOMAIN         certificate domain for http/dns/self/file
  --keep-config                leave an existing /etc/FNode/config.json untouched. By default its
                               Nodes block is replaced by the panel flags (a backup is kept)
  --force                      accepted for older dashboard commands; replacing is now the default
  -y, --yes                    do not ask for confirmation (uninstall)
  --print-config               print the config.json the flags produce and exit (dry run)
  --existing PATH              with --print-config: print PATH merged with the flags instead
  -h, --help                   show this help
USAGE
}

# Arguments. Any of --mode/--panel/--token/--machine-id/--node-id switches the
# script to non-interactive mode: no read prompts at all.
action="install"
arg_version=""
arg_mode=""
arg_panel=""
arg_token=""
arg_machine_id=""
arg_node_id=""
arg_type=""
arg_cert_mode="none"
arg_cert_domain=""
arg_keep_config=false
arg_yes=false
arg_print=false
arg_existing=""
noninteractive=false

need_value() {
    if [[ -z "$2" || "$2" == --* ]]; then
        echo -e "${red}错误：${plain} $1 需要一个值 (missing value for $1)"
        exit 1
    fi
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --mode) need_value "$1" "$2"; arg_mode="$2"; noninteractive=true; shift 2 ;;
        --mode=*) arg_mode="${1#*=}"; noninteractive=true; shift ;;
        --panel|--api-host) need_value "$1" "$2"; arg_panel="$2"; noninteractive=true; shift 2 ;;
        --panel=*|--api-host=*) arg_panel="${1#*=}"; noninteractive=true; shift ;;
        --token) need_value "$1" "$2"; arg_token="$2"; noninteractive=true; shift 2 ;;
        --token=*) arg_token="${1#*=}"; noninteractive=true; shift ;;
        --machine-id) need_value "$1" "$2"; arg_machine_id="$2"; noninteractive=true; shift 2 ;;
        --machine-id=*) arg_machine_id="${1#*=}"; noninteractive=true; shift ;;
        --node-id) need_value "$1" "$2"; arg_node_id="$2"; noninteractive=true; shift 2 ;;
        --node-id=*) arg_node_id="${1#*=}"; noninteractive=true; shift ;;
        --type) need_value "$1" "$2"; arg_type="$2"; shift 2 ;;
        --type=*) arg_type="${1#*=}"; shift ;;
        --version) need_value "$1" "$2"; arg_version="$2"; shift 2 ;;
        --version=*) arg_version="${1#*=}"; shift ;;
        --branch) need_value "$1" "$2"; FNODE_BRANCH="$2"; shift 2 ;;
        --branch=*) FNODE_BRANCH="${1#*=}"; shift ;;
        --cert-mode) need_value "$1" "$2"; arg_cert_mode="$2"; shift 2 ;;
        --cert-mode=*) arg_cert_mode="${1#*=}"; shift ;;
        --cert-domain) need_value "$1" "$2"; arg_cert_domain="$2"; shift 2 ;;
        --cert-domain=*) arg_cert_domain="${1#*=}"; shift ;;
        --force) shift ;;
        --keep-config) arg_keep_config=true; shift ;;
        --existing) need_value "$1" "$2"; arg_existing="$2"; shift 2 ;;
        --existing=*) arg_existing="${1#*=}"; shift ;;
        -y|--yes) arg_yes=true; shift ;;
        --print-config) arg_print=true; shift ;;
        -h|--help) usage; exit 0 ;;
        uninstall) action="uninstall"; shift ;;
        install) shift ;;
        -*) echo -e "${red}错误：${plain} 未知参数 (unknown option) $1"; usage; exit 1 ;;
        *)
            # Old positional version argument: install.sh 0.0.21 / pre / latest
            if [[ -z "$arg_version" ]]; then
                arg_version="$1"
            fi
            shift
            ;;
    esac
done

if [[ "$noninteractive" == true && "$action" == "install" ]]; then
    case "$arg_mode" in
        machine)
            [[ "$arg_machine_id" =~ ^[0-9]+$ && "$arg_machine_id" -gt 0 ]] || { echo -e "${red}错误：${plain} --mode machine 需要 --machine-id <正整数>"; exit 1; }
            ;;
        node)
            [[ "$arg_node_id" =~ ^[0-9]+$ && "$arg_node_id" -gt 0 ]] || { echo -e "${red}错误：${plain} --mode node 需要 --node-id <正整数>"; exit 1; }
            if [[ -n "$arg_type" ]]; then
                case "$arg_type" in
                    vless|vmess|v2ray|trojan|shadowsocks|hysteria|hysteria2|tuic|anytls) ;;
                    *) echo -e "${red}错误：${plain} 不支持的 --type $arg_type"; exit 1 ;;
                esac
            fi
            ;;
        *)
            echo -e "${red}错误：${plain} 需要 --mode machine 或 --mode node"; usage; exit 1
            ;;
    esac
    [[ "$arg_panel" =~ ^https?:// ]] || { echo -e "${red}错误：${plain} --panel 必须是 http:// 或 https:// 开头的面板地址"; exit 1; }
    [[ -n "$arg_token" ]] || { echo -e "${red}错误：${plain} 需要 --token"; exit 1; }
    case "$arg_cert_mode" in
        none|http|dns|self|file) ;;
        *) echo -e "${red}错误：${plain} --cert-mode 只能是 none/http/dns/self/file"; exit 1 ;;
    esac
    arg_panel="${arg_panel%/}"
    [[ -z "$arg_version" ]] && arg_version="latest"
fi

json_escape() {
    printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

has_ipv6() {
    ip -6 addr show scope global 2>/dev/null | grep -q "inet6" && ip -6 route show default 2>/dev/null | grep -q "default"
}

# Prints the Nodes entry for the selected mode (flat ApiConfig + Options).
panel_node_entry() {
    local listen_ip="0.0.0.0"
    has_ipv6 && listen_ip="::"
    local api
    if [[ "$arg_mode" == "machine" ]]; then
        api="\"ApiHost\": \"$(json_escape "$arg_panel")\",
            \"MachineID\": ${arg_machine_id},
            \"MachineToken\": \"$(json_escape "$arg_token")\","
    else
        api="\"ApiHost\": \"$(json_escape "$arg_panel")\",
            \"ApiKey\": \"$(json_escape "$arg_token")\",
            \"NodeID\": ${arg_node_id},"
        if [[ -n "$arg_type" ]]; then
            api="${api}
            \"NodeType\": \"$(json_escape "$arg_type")\","
        fi
    fi
    cat <<EOF
        {
            "Core": "sing",
            ${api}
            "Timeout": 30,
            "ListenIP": "${listen_ip}",
            "SendIP": "0.0.0.0",
            "DisableIPv6": true,
            "DeviceOnlineMinTraffic": 200,
            "EnableSniff": true,
            "CertConfig": {
                "CertMode": "$(json_escape "$arg_cert_mode")",
                "RejectUnknownSni": false,
                "CertDomain": "$(json_escape "${arg_cert_domain:-example.com}")",
                "CertFile": "/etc/FNode/fullchain.cer",
                "KeyFile": "/etc/FNode/cert.key",
                "Email": "fnode@github.com",
                "Provider": "cloudflare",
                "DNSEnv": {
                    "EnvName": "env1"
                }
            }
        }
EOF
}

# Prints a full config.json with the given Nodes entry.
render_config() {
    local entry="$1"
    cat <<EOF
{
    "Log": {
        "Level": "info",
        "Output": ""
    },
    "Cores": [
        {
            "Type": "sing",
            "Log": {
                "Level": "warn",
                "Timestamp": true
            },
            "DisableIPv6": true,
            "DomainStrategy": "prefer_ipv4",
            "NTP": {
                "Enable": true,
                "Server": "time.apple.com",
                "ServerPort": 0
            },
            "OriginalPath": "/etc/FNode/sing_origin.json"
        }
    ],
    "Nodes": [
${entry}
    ]
}
EOF
}

# Prints the config at $1 with its Nodes replaced by the entry in $2, keeping
# Log/Cores. A FNode log level of error (or quieter) is raised to info so the
# panel connection shows in the journal. Fails when jq is missing or the file
# is not plain JSON (JSON5 comments).
merge_panel_config() {
    command -v jq >/dev/null 2>&1 || return 1
    jq -e . "$1" >/dev/null 2>&1 || return 1
    printf '%s' "$2" | jq -s --slurpfile cfg "$1" '
        $cfg[0] + {Nodes: .}
        | if ((.Log.Level // "") | ascii_downcase) as $l
             | ["error", "fatal", "panic"] | index($l)
          then .Log.Level = "info" else . end'
}

# --print-config: show the config the flags would produce and exit (no root,
# nothing installed). Used by the tests and for a dry run.
if [[ "$arg_print" == true ]]; then
    if [[ "$noninteractive" != true ]]; then
        echo "--print-config needs --mode, --panel, --token and --machine-id/--node-id" >&2
        exit 1
    fi
    if [[ -n "$arg_existing" ]]; then
        merge_panel_config "$arg_existing" "$(panel_node_entry)" || { echo "cannot merge $arg_existing (needs jq and plain JSON)" >&2; exit 1; }
        exit 0
    fi
    render_config "$(panel_node_entry)"
    exit 0
fi

# check root
[[ $EUID -ne 0 ]] && echo -e "${red}错误：${plain} 必须使用root用户运行此脚本！\n" && exit 1

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

arch=$(uname -m)

if [[ $arch == "x86_64" || $arch == "x64" || $arch == "amd64" ]]; then
    arch="64"
elif [[ $arch == "aarch64" || $arch == "arm64" ]]; then
    arch="arm64-v8a"
elif [[ $arch == "s390x" ]]; then
    arch="s390x"
else
    arch="64"
    echo -e "${red}检测架构失败，使用默认架构: ${arch}${plain}"
fi

echo "架构: ${arch}"

if [ "$(getconf WORD_BIT)" != '32' ] && [ "$(getconf LONG_BIT)" != '64' ] ; then
    echo "本软件不支持 32 位系统(x86)，请使用 64 位系统(x86_64)，如果检测有误，请联系作者"
    exit 2
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
elif [[ x"${release}" == x"ubuntu" ]]; then
    if [[ ${os_version} -lt 16 ]]; then
        echo -e "${red}请使用 Ubuntu 16 或更高版本的系统！${plain}\n" && exit 1
    fi
elif [[ x"${release}" == x"debian" ]]; then
    if [[ ${os_version} -lt 8 ]]; then
        echo -e "${red}请使用 Debian 8 或更高版本的系统！${plain}\n" && exit 1
    fi
fi

# --- extract helpers ---
have_downloader() {
    command -v wget >/dev/null 2>&1 || command -v curl >/dev/null 2>&1
}

# unzip, or a fallback that can read a zip: bsdtar, python3, busybox
have_extractor() {
    command -v unzip >/dev/null 2>&1 && return 0
    command -v bsdtar >/dev/null 2>&1 && return 0
    command -v python3 >/dev/null 2>&1 && return 0
    command -v busybox >/dev/null 2>&1 && busybox unzip 2>&1 | grep -qi usage && return 0
    return 1
}

# extract_zip ZIP DIR: unzips ZIP into DIR with the first tool that works.
# geoip.dat/geosite.dat (older releases) are not used by sing-box 1.12+.
extract_zip() {
    local zip="$1" dest="$2"
    mkdir -p "$dest"
    if command -v unzip >/dev/null 2>&1; then
        unzip -o -q "$zip" -x geoip.dat geosite.dat -d "$dest" && return 0
    fi
    if command -v bsdtar >/dev/null 2>&1; then
        bsdtar -xf "$zip" -C "$dest" --exclude geoip.dat --exclude geosite.dat && return 0
    fi
    if command -v python3 >/dev/null 2>&1; then
        python3 -c 'import sys, zipfile
z = zipfile.ZipFile(sys.argv[1])
for n in z.namelist():
    if n not in ("geoip.dat", "geosite.dat"):
        z.extract(n, sys.argv[2])' "$zip" "$dest" && return 0
    fi
    if command -v busybox >/dev/null 2>&1 && busybox unzip 2>&1 | grep -qi usage; then
        busybox unzip -o "$zip" -d "$dest" >/dev/null && rm -f "$dest/geoip.dat" "$dest/geosite.dat" && return 0
    fi
    return 1
}

# download_file URL FILE: wget, then curl; fails on an HTTP error
download_file() {
    local url="$1" out="$2"
    if command -v wget >/dev/null 2>&1; then
        wget --no-check-certificate --tries=3 --progress=bar -O "$out" "$url" && return 0
    fi
    if command -v curl >/dev/null 2>&1; then
        curl -fL --retry 3 -o "$out" "$url" && return 0
    fi
    return 1
}
# --- end extract helpers ---

# pkg_install PACKAGE...: installs with the system package manager, showing
# errors (only stdout is silenced)
pkg_install() {
    if [[ x"${release}" == x"centos" ]]; then
        local pkgs=("${@/cron/crontabs}")
        [[ " $* " == *" jq "* ]] && yum install epel-release -y >/dev/null
        yum install "${pkgs[@]}" -y >/dev/null
        update-ca-trust force-enable >/dev/null 2>&1
    elif [[ x"${release}" == x"alpine" ]]; then
        apk add --no-cache "$@" >/dev/null
        update-ca-certificates >/dev/null 2>&1
    elif [[ x"${release}" == x"debian" || x"${release}" == x"ubuntu" ]]; then
        apt-get update -y >/dev/null
        DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@" >/dev/null
        update-ca-certificates >/dev/null 2>&1
    elif [[ x"${release}" == x"arch" ]]; then
        local pkgs=("${@/cron/cronie}")
        pacman -Sy --noconfirm --needed "${pkgs[@]}" >/dev/null
    fi
}

# Installs only the packages that are missing and skips the package manager
# when nothing is (a small VPS saves time, memory and disk). socat is no
# longer installed: FNode requests certificates itself (lego). The install
# stops here, with the package manager's message, when it still cannot
# download or unpack a zip afterwards.
install_base() {
    local missing=() c
    for c in wget curl unzip tar jq; do
        command -v "$c" >/dev/null 2>&1 || missing+=("$c")
    done
    # cron is only used for the weekly log cleanup
    if [[ x"${release}" != x"alpine" ]] && ! command -v crontab >/dev/null 2>&1; then
        missing+=("cron")
    fi
    if [[ ! -s /etc/ssl/certs/ca-certificates.crt && ! -s /etc/pki/tls/certs/ca-bundle.crt && ! -s /etc/ssl/cert.pem ]]; then
        missing+=("ca-certificates")
    fi
    if [[ ${#missing[@]} -eq 0 ]]; then
        echo -e "依赖已齐全，跳过安装 (dependencies present, skipping package install)"
        return 0
    fi
    echo -e "安装依赖 (installing): ${missing[*]}"
    pkg_install "${missing[@]}"
    if ! have_downloader || ! have_extractor; then
        echo -e "${yellow}依赖安装未完成，重试一次 (dependencies still missing, retrying once; package manager output follows)${plain}"
        pkg_install "${missing[@]}" 2>&1
    fi
    if ! have_downloader; then
        echo -e "${red}错误：${plain} 没有 wget 或 curl，无法下载 FNode (neither wget nor curl could be installed; check the network, free disk space and the package manager output above)"
        exit 1
    fi
    if ! have_extractor; then
        echo -e "${red}错误：${plain} 没有 unzip，也没有 bsdtar/python3/busybox，无法解压 (no unzip, bsdtar, python3 or busybox: install one by hand, e.g. apt-get install unzip, then run this again)"
        exit 1
    fi
    command -v jq >/dev/null 2>&1 || echo -e "${yellow}未安装 jq：已有 config.json 无法合并，将被整体重写 (jq missing: an existing config.json is rewritten, not merged)${plain}"
}

# Total RAM in MiB (0 when unknown).
total_mem_mb() {
    awk '/^MemTotal:/ {printf "%d", $2/1024}' /proc/meminfo 2>/dev/null || echo 0
}

# Go GC limit sized to the machine: on small VPSes the Go runtime collects
# harder before the heap reaches the RAM, instead of the kernel OOM killer
# picking a victim. Prints nothing on machines with 2 GB or more.
fnode_gomemlimit() {
    local mem
    mem=$(total_mem_mb)
    if [[ "${mem:-0}" -gt 0 && "$mem" -lt 2048 ]]; then
        echo "$((mem * 70 / 100))MiB"
    fi
    return 0
}

# systemd [Service] lines for the memory limits; below 1.5 GB RAM FNode is
# also capped in its own cgroup, so an OOM restarts FNode (Restart=always)
# and not sshd.
fnode_unit_memory_lines() {
    local mem limit
    mem=$(total_mem_mb)
    limit=$(fnode_gomemlimit)
    [[ -n "$limit" ]] && echo "Environment=GOMEMLIMIT=${limit}"
    [[ "${mem:-0}" -gt 0 && "$mem" -lt 1536 ]] && echo "MemoryMax=$((mem * 85 / 100))M"
    return 0
}

# Advice for a small machine without swap (printed, nothing is changed).
small_vps_hints() {
    local mem swap
    mem=$(total_mem_mb)
    swap=$(awk '/^SwapTotal:/ {print $2}' /proc/meminfo 2>/dev/null)
    if [[ "${mem:-0}" -gt 0 && "$mem" -lt 1024 && "${swap:-0}" -eq 0 ]]; then
        echo -e "${yellow}内存只有 ${mem} MB 且没有 swap，建议创建 512 MB swap 防止被 OOM 杀掉 (small RAM and no swap; a swapfile avoids OOM kills):${plain}"
        echo "  fallocate -l 512M /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile && echo '/swapfile none swap sw 0 0' >> /etc/fstab"
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

setup_cleanlog_cron() {
    echo -e "${green}配置 FNode 每 7 天自动清理日志定时任务...${plain}"
    if [[ -d "/etc/cron.d" ]]; then
        cat << 'EOF' > /etc/cron.d/FNode-cleanlog
# Auto clean up FNode logs every 7 days at 03:00
0 3 */7 * * root /usr/bin/FNode clearlog >/dev/null 2>&1
EOF
        chmod 644 /etc/cron.d/FNode-cleanlog
    elif [[ -d "/etc/periodic/weekly" ]]; then
        cat << 'EOF' > /etc/periodic/weekly/fnode-cleanlog
#!/bin/sh
/usr/bin/FNode clearlog >/dev/null 2>&1
EOF
        chmod +x /etc/periodic/weekly/fnode-cleanlog
    else
        (crontab -l 2>/dev/null | grep -v "FNode clearlog" ; echo "0 3 */7 * * /usr/bin/FNode clearlog >/dev/null 2>&1") | crontab - 2>/dev/null || true
    fi
}

write_default_sing_origin() {
    [[ -f /etc/FNode/sing_origin.json ]] && return 0
    # FNode default outbounds and routes (same as the built-in default)
    cat <<'EOF' > /etc/FNode/sing_origin.json
{
  "outbounds": [
    {
      "tag": "direct",
      "type": "direct",
      "domain_strategy": "prefer_ipv4"
    },
    {
      "tag": "direct6",
      "type": "direct",
      "domain_strategy": "prefer_ipv6"
    },
    {
      "type": "block",
      "tag": "block"
    }
  ],
  "route": {
    "auto_detect_interface": true,
    "rules": [
      {
        "ip_is_private": true,
        "outbound": "block"
      },
      {
        "protocol": [
          "bittorrent"
        ],
        "outbound": "block"
      },
      {
        "port": [
          25,
          465,
          587
        ],
        "network": [
          "tcp"
        ],
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
          "(..||)(visa|mycard|gash|beanfun).",
          "(.*.||)(gov|12377|12315|talk.news.pts.org|creaders|zhuichaguoji|efcc.org|cyberpolice|aboluowang|tuidang|epochtimes|zhengjian|110.qq|mingjingnews|inmediahk|xinsheng|breakgfw|chengmingmag|jinpianwang|qi-gong|mhradio|edoors|renminbao|soundofhope|xizang-zhiye|bannedbook|ntdtv|12321|secretchina|dajiyuan|boxun|chinadigitaltimes|dwnews|huaglad|oneplusnews|epochweekly|cn.rfi).(cn|com|org|net|club|net|fr|tw|hk|eu|info|me)",
          "(.*.||)(miaozhen|cnzz|talkingdata|umeng).(cn|com)",
          "(.*.||)(mycard).(com|tw)",
          "(.*.||)(gash).(com|tw)",
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
          "udp",
          "tcp"
        ]
      }
    ]
  }
}
EOF
}

# Writes /etc/FNode/config.json for --mode machine|node. The dashboard flags
# win: an existing config is backed up and its Nodes block replaced (the
# whole file when jq is missing or cannot parse it). --keep-config leaves it.
write_panel_config() {
    mkdir -p /etc/FNode
    write_default_sing_origin
    local entry
    entry=$(panel_node_entry)
    if [[ "$had_config" == true && "$arg_keep_config" == true ]]; then
        echo -e "${yellow}已存在 /etc/FNode/config.json，未修改 (--keep-config: existing config kept, the --panel/--token flags are NOT applied)${plain}"
        return 0
    fi
    if [[ -f /etc/FNode/config.json ]]; then
        local backup
        backup="/etc/FNode/config.json.bak.$(date +%Y%m%d%H%M%S)"
        cp /etc/FNode/config.json "$backup"
        echo -e "${yellow}已备份旧配置 (old config backed up): ${backup}${plain}"
    fi
    if [[ "$had_config" == true ]]; then
        local tmp
        tmp=$(mktemp /etc/FNode/.config.json.XXXXXX.tmp)
        if merge_panel_config /etc/FNode/config.json "$entry" > "$tmp" 2>/dev/null; then
            mv -f "$tmp" /etc/FNode/config.json
            chmod 600 /etc/FNode/config.json
            echo -e "${green}已替换 /etc/FNode/config.json 的 Nodes 配置为面板 ${arg_panel} (${arg_mode} mode; panel block replaced, Log/Cores kept)${plain}"
            return 0
        fi
        rm -f "$tmp"
        echo -e "${yellow}无法合并旧配置 (jq missing or config is not plain JSON): rewriting the whole file, Log/Cores reset to defaults${plain}"
    fi
    render_config "$entry" > /etc/FNode/config.json
    chmod 600 /etc/FNode/config.json
    echo -e "${green}已生成 /etc/FNode/config.json (${arg_mode} mode)${plain}"
}

start_service() {
    if [[ x"${release}" == x"alpine" ]]; then
        service FNode restart
    else
        systemctl restart FNode
    fi
    sleep 3
    check_status
    if [[ $? == 0 ]]; then
        echo -e "${green}FNode 已启动 (running)${plain}"
        return 0
    fi
    echo -e "${red}FNode 启动失败 (FNode is not running). Last log lines:${plain}"
    if [[ x"${release}" != x"alpine" ]]; then
        journalctl -u FNode -n 15 --no-pager 2>/dev/null
    fi
    echo -e "${red}请检查 /etc/FNode/config.json 和面板地址/令牌，然后运行: FNode restart; FNode log${plain}"
    return 1
}

uninstall_FNode() {
    if [[ "$arg_yes" != true ]]; then
        if [ -t 0 ]; then
            read -rp "确定要卸载 FNode 吗? (uninstall FNode) [y/N]: " confirm_uninstall
            [[ "$confirm_uninstall" == [Yy] ]] || { echo "已取消 (cancelled)"; exit 0; }
        else
            echo -e "${red}错误：${plain} 非交互模式卸载需要 --yes (pass --yes to uninstall without a terminal)"
            exit 1
        fi
    fi
    if [[ x"${release}" == x"alpine" ]]; then
        service FNode stop 2>/dev/null
        rc-update del FNode 2>/dev/null
        rm /etc/init.d/FNode -f
    else
        systemctl stop FNode 2>/dev/null
        systemctl disable FNode 2>/dev/null
        rm /etc/systemd/system/FNode.service -f
        systemctl daemon-reload
        systemctl reset-failed 2>/dev/null
    fi
    rm /etc/FNode/ -rf
    rm /usr/local/FNode/ -rf
    rm -f /etc/cron.d/FNode-cleanlog /etc/periodic/weekly/fnode-cleanlog
    (crontab -l 2>/dev/null | grep -v "FNode clearlog") | crontab - 2>/dev/null || true
    rm -f /usr/bin/FNode /usr/bin/fnode
    echo -e "${green}FNode 已卸载 (uninstalled)${plain}"
}

install_FNode() {

    # Download and verify in a staging directory; the installed FNode is only
    # replaced once the new binary runs, so a failed update leaves it working.
    stage=/usr/local/FNode.new
    rm -rf "$stage"
    mkdir -p "$stage"
    trap 'rm -rf /usr/local/FNode.new' EXIT
    cd "$stage"

    target_type="$1"
    if [[ -z "$target_type" ]]; then
        if [ -t 0 ]; then
            echo -e "请选择要安装的 FNode 版本:"
            echo -e "  ${green}1.${plain} 最新正式版 (Latest Release, 默认)"
            echo -e "  ${green}2.${plain} 预发布预览版 (Pre-release)"
            echo -e "  ${green}3.${plain} 自定义指定版本 (Specific Version)"
            read -rp "请输入选择 [1-3] (默认 1): " ver_choice
            case "$ver_choice" in
                2)
                    target_type="pre"
                    ;;
                3)
                    read -rp "请输入指定版本号 (例如 0.0.21 或 0.0.22-pre): " custom_ver
                    target_type="${custom_ver}"
                    ;;
                1|"")
                    target_type="latest"
                    ;;
                *)
                    echo -e "${yellow}输入无效，默认安装最新正式版${plain}"
                    target_type="latest"
                    ;;
            esac
        else
            target_type="latest"
        fi
    fi

    if [[ "$target_type" == "pre" || "$target_type" == "prerelease" || "$target_type" == "pre-release" ]]; then
        echo -e "正在获取最新预发布版本信息..."
        last_version=$(curl -Ls "https://api.github.com/repos/tavut846/FNode/releases" | awk -F'"' '/"tag_name":/{t=$4} /"prerelease": *true/{print t; exit}')
        if [[ ! -n "$last_version" ]]; then
            echo -e "${red}检测 FNode 预发布版本失败，可能是超出 Github API 限制或暂无预发布版本，请稍后再试或手动指定版本安装${plain}"
            exit 1
        fi
        echo -e "检测到 FNode 最新预发布版本：${green}${last_version}${plain}，开始安装"
        url="https://github.com/tavut846/FNode/releases/download/${last_version}/FNode-linux-${arch}.zip"
    elif [[ "$target_type" == "latest" ]]; then
        echo -e "正在获取最新正式版信息..."
        last_version=$(curl -Ls "https://api.github.com/repos/tavut846/FNode/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
        if [[ ! -n "$last_version" ]]; then
            echo -e "${red}检测 FNode 版本失败，可能是超出 Github API 限制，请稍后再试，或手动指定 FNode 版本安装${plain}"
            exit 1
        fi
        echo -e "检测到 FNode 最新正式版本：${green}${last_version}${plain}，开始安装"
        url="https://github.com/tavut846/FNode/releases/download/${last_version}/FNode-linux-${arch}.zip"
    else
        last_version="$target_type"
        url="https://github.com/tavut846/FNode/releases/download/${last_version}/FNode-linux-${arch}.zip"
        echo -e "开始安装指定版本 FNode: ${green}${last_version}${plain}"
    fi

    # the zip and the extracted binary are on disk together: about 100 MB
    local free_mb
    free_mb=$(df -Pm /usr/local 2>/dev/null | awk 'NR==2 {print $4}')
    if [[ -n "$free_mb" && "$free_mb" -lt 120 ]]; then
        echo -e "${red}磁盘空间不足：/usr/local 只剩 ${free_mb} MB，至少需要 120 MB (not enough free disk: ${free_mb} MB left, need 120 MB). Free some space (apt-get clean, journalctl --vacuum-size=20M) and retry${plain}"
        exit 1
    fi
    if ! download_file "${url}" "$stage/FNode-linux.zip"; then
        echo -e "${red}下载 FNode ${last_version} 失败，请确保你的服务器能够下载 Github 的文件且此版本存在 (download failed: ${url}). Nothing was changed.${plain}"
        exit 1
    fi
    if [[ $(wc -c < "$stage/FNode-linux.zip") -lt 1000000 ]]; then
        echo -e "${red}下载的文件太小，不是 FNode 安装包 (the download is not a FNode release zip; wrong version or arch?): ${url}. Nothing was changed.${plain}"
        exit 1
    fi
    if ! extract_zip "$stage/FNode-linux.zip" "$stage"; then
        echo -e "${red}解压失败：压缩包损坏或磁盘已满 (could not extract the zip: it is damaged or the disk is full). Nothing was changed. Free space and run again.${plain}"
        exit 1
    fi
    rm -f "$stage/FNode-linux.zip"
    if [[ ! -s "$stage/FNode" ]]; then
        echo -e "${red}压缩包里没有 FNode 程序 (the zip has no FNode binary). Nothing was changed.${plain}"
        exit 1
    fi
    chmod +x "$stage/FNode"
    if ! ver_out=$("$stage/FNode" version --short 2>&1); then
        echo -e "${red}FNode 无法运行 (the downloaded binary does not run on this machine, ${arch} / $(uname -m)): ${ver_out}. Nothing was changed.${plain}"
        exit 1
    fi
    for f in dns.json route.json custom_outbound.json custom_inbound.json; do
        [[ -f "$stage/$f" ]] || echo -e "${yellow}压缩包缺少 $f (zip lacks $f)${plain}"
    done
    # swap in the verified install, keep the old one until it is in place
    rm -rf /usr/local/FNode.old
    [[ -e /usr/local/FNode ]] && mv /usr/local/FNode /usr/local/FNode.old
    if ! mv "$stage" /usr/local/FNode; then
        [[ -e /usr/local/FNode.old ]] && mv /usr/local/FNode.old /usr/local/FNode
        echo -e "${red}无法移动到 /usr/local/FNode (could not move the new install into place); the old one was restored${plain}"
        exit 1
    fi
    rm -rf /usr/local/FNode.old
    trap - EXIT
    cd /usr/local/FNode
    echo -e "FNode 程序已验证 (binary verified): ${green}${ver_out}${plain}"
    mkdir /etc/FNode/ -p
    if [[ x"${release}" == x"alpine" ]]; then
        rm /etc/init.d/FNode -f
        cat <<EOF > /etc/init.d/FNode
#!/sbin/openrc-run

name="FNode"
description="FNode"

$(l=$(fnode_gomemlimit); [ -n "$l" ] && echo "export GOMEMLIMIT=$l")
command="/usr/local/FNode/FNode"
command_args="server"
command_user="root"

pidfile="/run/FNode.pid"
command_background="yes"

depend() {
        need net
}

start_pre() {
        if [ ! -x /usr/local/FNode/FNode ]; then
                eerror "/usr/local/FNode/FNode is missing: reinstall FNode"
                return 1
        fi
}
EOF
        chmod +x /etc/init.d/FNode
        rc-update add FNode default
        echo -e "${green}FNode ${last_version}${plain} 安装完成，已设置开机自启"
    else
        rm /etc/systemd/system/FNode.service -f
        cat <<EOF > /etc/systemd/system/FNode.service
[Unit]
Description=FNode Service
After=network.target nss-lookup.target
Wants=network.target
# do not restart forever when the binary is missing (a broken install)
ConditionPathIsExecutable=/usr/local/FNode/FNode

[Service]
User=root
Group=root
Type=simple
LimitAS=infinity
LimitRSS=infinity
LimitCORE=infinity
LimitNOFILE=1048576
LimitNPROC=512000
WorkingDirectory=/usr/local/FNode/
ExecStart=/usr/local/FNode/FNode server
Restart=always
RestartSec=10
$(fnode_unit_memory_lines)

[Install]
WantedBy=multi-user.target
EOF
        systemctl daemon-reload
        systemctl stop FNode
        systemctl enable FNode
        echo -e "${green}FNode ${last_version}${plain} 安装完成，已设置开机自启"
    fi

    if [[ "$noninteractive" == true ]]; then
        write_panel_config
        first_install=false
    elif [[ ! -f /etc/FNode/config.json ]]; then
        cp config.json /etc/FNode/
        echo -e ""
        first_install=true
    else
        if [[ x"${release}" == x"alpine" ]]; then
            service FNode start
        else
            systemctl start FNode
        fi
        sleep 2
        check_status
        local started=$?
        echo -e ""
        if [[ $started == 0 ]]; then
            echo -e "${green}FNode 重启成功${plain}"
        else
            echo -e "${red}FNode 可能启动失败，请稍后使用 FNode log 查看日志信息，若无法启动，则可能更改了配置格式，请前往 wiki 查看：https://github.com/FNode-project/FNode/wiki${plain}"
        fi
        first_install=false
    fi

    if [[ ! -f /etc/FNode/dns.json ]]; then
        cp dns.json /etc/FNode/
    fi
    if [[ ! -f /etc/FNode/route.json ]]; then
        cp route.json /etc/FNode/
    fi
    if [[ ! -f /etc/FNode/custom_outbound.json ]]; then
        cp custom_outbound.json /etc/FNode/
    fi
    if [[ ! -f /etc/FNode/custom_inbound.json ]]; then
        cp custom_inbound.json /etc/FNode/
    fi
    curl -o /usr/bin/FNode -Ls "https://raw.githubusercontent.com/tavut846/FNode/${FNODE_BRANCH}/FNode-script/FNode.sh"
    chmod +x /usr/bin/FNode
    if [ ! -L /usr/bin/fnode ]; then
        ln -s /usr/bin/FNode /usr/bin/fnode
        chmod +x /usr/bin/fnode
    fi
    setup_cleanlog_cron
    small_vps_hints
    cd $cur_dir
    rm -f install.sh
    echo -e ""
    echo "FNode 管理脚本使用方法 (兼容使用FNode执行，大小写不敏感): "
    echo "------------------------------------------"
    echo "FNode              - 显示管理菜单 (功能更多)"
    echo "FNode start        - 启动 FNode"
    echo "FNode stop         - 停止 FNode"
    echo "FNode restart      - 重启 FNode"
    echo "FNode status       - 查看 FNode 状态 (含面板地址、令牌、节点标签)"
    echo "FNode enable       - 设置 FNode 开机自启"
    echo "FNode disable      - 取消 FNode 开机自启"
    echo "FNode log          - 查看 FNode 日志"
    echo "FNode x25519       - 生成 x25519 密钥"
    echo "FNode generate     - 生成 FNode 配置文件"
    echo "FNode update       - 更新 FNode (交互式选择: 最新版/预发布版/指定版本)"
    echo "FNode update pre   - 更新 FNode 至最新预发布预览版"
    echo "FNode update x.x.x - 更新 FNode 指定版本"
    echo "FNode install      - 安装 FNode (可选参数: latest/pre/指定版本)"
    echo "FNode uninstall    - 卸载 FNode (--yes 跳过确认)"
    echo "FNode config show  - 查看生效配置与出站来源 (--node 标签 --summary)"
    echo "FNode config status - 查看面板地址、令牌(部分)与节点标签"
    echo "FNode version      - 查看 FNode 版本"
    echo "------------------------------------------"
    if [[ "$noninteractive" == true ]]; then
        start_service
        return $?
    fi
    # 首次安装询问是否生成配置文件 (only with a terminal: piped stdin is the script itself)
    if [[ $first_install == true && -t 0 ]]; then
        read -rp "检测到你为第一次安装FNode,是否自动直接生成配置文件？(y/n): " if_generate
        if [[ $if_generate == [Yy] ]]; then
            curl -o ./initconfig.sh -Ls "https://raw.githubusercontent.com/tavut846/FNode/${FNODE_BRANCH}/FNode-script/initconfig.sh"
            source initconfig.sh
            rm initconfig.sh -f
            generate_config_file
        fi
    fi
}

if [[ "$action" == "uninstall" ]]; then
    uninstall_FNode
    exit 0
fi

had_config=false
[[ -f /etc/FNode/config.json ]] && had_config=true

echo -e "${green}开始安装${plain}"
install_base
install_FNode "$arg_version"
