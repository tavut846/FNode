<?php

namespace App\Protocols;

use App\Models\Server;
use App\Support\AbstractProtocol;
use App\Utils\Helper;

class Egern extends AbstractProtocol
{
    public $flags = ['egern'];
    const CUSTOM_TEMPLATE_FILE = 'resources/rules/custom.egern.yaml';
    const DEFAULT_TEMPLATE_FILE = 'resources/rules/default.egern.yaml';
    public $allowedProtocols = [
        Server::TYPE_SHADOWSOCKS,
        Server::TYPE_VMESS,
        Server::TYPE_TROJAN,
        Server::TYPE_HYSTERIA,
        Server::TYPE_TUIC,
        Server::TYPE_ANYTLS,
        Server::TYPE_SOCKS,
        Server::TYPE_HTTP,
        Server::TYPE_MIERU,
    ];

    public function handle()
    {
        $servers = $this->servers;
        $user = $this->user;
        $appName = admin_setting('app_name', 'XBoard');
        $userEmail = $user['email'];

        $config = subscribe_template('egern');
        if (empty($config)) {
            $templatePath = base_path('resources/rules/default.egern.yaml');
            if (file_exists($templatePath)) {
                $config = file_get_contents($templatePath);
            }
        }

        $proxies = '';
        $proxyGroupArry = [];

        foreach ($servers as $item) {
            $proxy = '';
            switch ($item['type']) {
                case Server::TYPE_SHADOWSOCKS:
                    $proxy = self::buildShadowsocks($item['password'], $item);
                    break;
                case Server::TYPE_VMESS:
                    $proxy = self::buildVmess($item['password'], $item);
                    break;
                case Server::TYPE_TROJAN:
                    $proxy = self::buildTrojan($item['password'], $item);
                    break;
                case Server::TYPE_HYSTERIA:
                    $proxy = self::buildHysteria($item['password'], $item);
                    break;
                case Server::TYPE_TUIC:
                    $proxy = self::buildTuic($item['password'], $item);
                    break;
                case Server::TYPE_ANYTLS:
                    $proxy = self::buildAnyTLS($item['password'], $item);
                    break;
                case Server::TYPE_SOCKS:
                    $proxy = self::buildSocks($item['password'], $item);
                    break;
                case Server::TYPE_HTTP:
                    $proxy = self::buildHttp($item['password'], $item);
                    break;
                case Server::TYPE_MIERU:
                    $proxy = self::buildMieru($item['password'], $item);
                    break;
            }

            if ($proxy) {
                $proxies .= $proxy;
                array_push($proxyGroupArry, $item['name']);
            }
        }
        
        $proxyGroup = "";
        if (!empty($proxyGroupArry)) {
            $proxyGroup = "    - " . implode("\n    - ", $proxyGroupArry);
        }
        
        $config = str_replace('$proxies', rtrim($proxies, "\n"), $config);
        $config = str_replace('$proxy_group', $proxyGroup, $config);

        // Subscription link
        $subsDomain = request()->header('Host');
        $subsURL = Helper::getSubscribeUrl($user['token'], $subsDomain ? 'https://' . $subsDomain : null);
        
        $config = str_replace('$subs_link', $subsURL, $config);
        $config = str_replace('$subs_domain', $subsDomain, $config);
        $config = str_replace('$encode_subs_link', rawurlencode($subsURL), $config);
        $config = str_replace('$app_name', $appName, $config);
        $config = str_replace('$user_email', $userEmail, $config);

        return response($config)
            ->header('content-type', 'application/yaml')
            ->header('subscription-userinfo', "upload={$user['u']}; download={$user['d']}; total={$user['transfer_enable']}; expire={$user['expired_at']}")
            ->header('profile-update-interval', '72')
            ->header('content-disposition', "attachment;filename*=UTF-8''" . rawurlencode($appName) . '.yaml')
            ->header('profile-web-page-url', admin_setting('app_url'));
    }

    public static function buildShadowsocks($password, $server)
    {
        $protocol_settings = $server['protocol_settings'];
        $config = [];
        $config[] = "- shadowsocks:";
        $config[] = "    name: \"{$server['name']}\"";
        $config[] = "    server: {$server['host']}";
        $config[] = "    port: {$server['port']}";
        $config[] = "    method: " . data_get($protocol_settings, 'cipher');
        $config[] = "    password: " . data_get($server, 'password', $password);
        $config[] = "    udp_relay: true";
        
        if ($plugin = data_get($protocol_settings, 'plugin')) {
            $config[] = "    obfs: " . ($plugin === 'obfs' ? 'http' : $plugin);
            if ($pluginOpts = data_get($protocol_settings, 'plugin_opts')) {
                $parsedOpts = collect(explode(';', $pluginOpts))
                    ->filter()
                    ->mapWithKeys(function ($pair) {
                        if (!str_contains($pair, '=')) return [trim($pair) => true];
                        [$key, $value] = explode('=', $pair, 2);
                        return [trim($key) => trim($value)];
                    })->all();
                
                if (isset($parsedOpts['obfs-host'])) $config[] = "    obfs_host: {$parsedOpts['obfs-host']}";
                if (isset($parsedOpts['path'])) $config[] = "    obfs_uri: \"{$parsedOpts['path']}\"";
            }
        }
        
        return implode("\n", $config) . "\n";
    }

    public static function buildVmess($uuid, $server)
    {
        $protocol_settings = $server['protocol_settings'];
        $config = [];
        $config[] = "- vmess:";
        $config[] = "    name: \"{$server['name']}\"";
        $config[] = "    server: {$server['host']}";
        $config[] = "    port: {$server['port']}";
        $config[] = "    user_id: {$uuid}";
        $config[] = "    security: auto";
        $config[] = "    udp_relay: true";
        
        $network = data_get($protocol_settings, 'network', 'tcp');
        $tls = data_get($protocol_settings, 'tls');
        
        if ($network === 'ws' || $tls) {
            $config[] = "    transport:";
            $transportKey = $network === 'ws' ? ($tls ? 'wss' : 'ws') : 'tls';
            $config[] = "        {$transportKey}:";
            if ($network === 'ws') {
                $config[] = "            path: \"" . data_get($protocol_settings, 'network_settings.path', '/') . "\"";
                if ($host = data_get($protocol_settings, 'network_settings.headers.Host')) {
                    $config[] = "            headers:";
                    $config[] = "                Host: {$host}";
                }
            }
            if ($tls) {
                if ($sni = data_get($protocol_settings, 'tls_settings.server_name')) {
                    $config[] = "            sni: {$sni}";
                }
                $config[] = "            skip_tls_verify: " . (data_get($protocol_settings, 'tls_settings.allow_insecure') ? 'true' : 'false');
            }
        }
        
        return implode("\n", $config) . "\n";
    }

    public static function buildTrojan($password, $server)
    {
        $protocol_settings = $server['protocol_settings'];
        $config = [];
        $config[] = "- trojan:";
        $config[] = "    name: \"{$server['name']}\"";
        $config[] = "    server: {$server['host']}";
        $config[] = "    port: {$server['port']}";
        $config[] = "    password: {$password}";
        $config[] = "    udp_relay: true";
        
        if ($sni = data_get($protocol_settings, 'server_name')) {
            $config[] = "    sni: {$sni}";
        }
        $config[] = "    skip_tls_verify: " . (data_get($protocol_settings, 'allow_insecure') ? 'true' : 'false');
        
        return implode("\n", $config) . "\n";
    }

    public static function buildHysteria($password, $server)
    {
        $protocol_settings = data_get($server, 'protocol_settings', []);
        $config = [];
        $version = (int) data_get($protocol_settings, 'version', 1);
        
        if ($version === 2) {
            $config[] = "- hysteria2:";
            $config[] = "    name: \"{$server['name']}\"";
            $config[] = "    server: {$server['host']}";
            $config[] = "    port: {$server['port']}";
            $config[] = "    auth: {$password}";
            if ($sni = data_get($protocol_settings, 'tls.server_name')) {
                $config[] = "    sni: {$sni}";
            }
            $config[] = "    skip_tls_verify: " . (data_get($protocol_settings, 'tls.allow_insecure') ? 'true' : 'false');
            if (data_get($protocol_settings, 'obfs.open')) {
                $config[] = "    obfs: " . data_get($protocol_settings, 'obfs.type', 'salamander');
                $config[] = "    obfs_password: " . data_get($protocol_settings, 'obfs.password');
            }
        } else {
            $config[] = "- hysteria:";
            $config[] = "    name: \"{$server['name']}\"";
            $config[] = "    server: {$server['host']}";
            $config[] = "    port: {$server['port']}";
            $config[] = "    auth: {$password}";
            if ($sni = data_get($protocol_settings, 'tls.server_name')) {
                $config[] = "    sni: {$sni}";
            }
            $config[] = "    skip_tls_verify: " . (data_get($protocol_settings, 'tls.allow_insecure') ? 'true' : 'false');
        }
        
        return implode("\n", $config) . "\n";
    }

    public static function buildTuic($password, $server)
    {
        $protocol_settings = data_get($server, 'protocol_settings', []);
        $config = [];
        $config[] = "- tuic:";
        $config[] = "    name: \"{$server['name']}\"";
        $config[] = "    server: {$server['host']}";
        $config[] = "    port: {$server['port']}";
        if (data_get($protocol_settings, 'version') === 4) {
            $config[] = "    token: {$password}";
        } else {
            $config[] = "    uuid: {$password}";
            $config[] = "    password: {$password}";
        }
        if ($sni = data_get($protocol_settings, 'tls.server_name')) {
            $config[] = "    sni: {$sni}";
        }
        $config[] = "    skip_tls_verify: " . (data_get($protocol_settings, 'tls.allow_insecure') ? 'true' : 'false');
        $config[] = "    udp_relay_mode: " . data_get($protocol_settings, 'udp_relay_mode', 'native');
        if ($alpn = data_get($protocol_settings, 'alpn')) {
            $config[] = "    alpn:";
            foreach ((array)$alpn as $v) $config[] = "      - {$v}";
        }
        
        return implode("\n", $config) . "\n";
    }

    public static function buildAnyTLS($password, $server)
    {
        $protocol_settings = data_get($server, 'protocol_settings', []);
        $config = [];
        $config[] = "- anytls:";
        $config[] = "    name: \"{$server['name']}\"";
        $config[] = "    server: {$server['host']}";
        $config[] = "    port: {$server['port']}";
        $config[] = "    password: {$password}";
        if ($sni = data_get($protocol_settings, 'tls.server_name')) {
            $config[] = "    sni: {$sni}";
        }
        $config[] = "    skip_tls_verify: " . (data_get($protocol_settings, 'tls.allow_insecure') ? 'true' : 'false');
        
        return implode("\n", $config) . "\n";
    }

    public static function buildSocks($password, $server)
    {
        $config = [];
        $config[] = "- socks5:";
        $config[] = "    name: \"{$server['name']}\"";
        $config[] = "    server: {$server['host']}";
        $config[] = "    port: {$server['port']}";
        $config[] = "    username: {$password}";
        $config[] = "    password: {$password}";
        $config[] = "    udp_relay: true";
        
        return implode("\n", $config) . "\n";
    }

    public static function buildHttp($password, $server)
    {
        $config = [];
        $config[] = "- http:";
        $config[] = "    name: \"{$server['name']}\"";
        $config[] = "    server: {$server['host']}";
        $config[] = "    port: {$server['port']}";
        $config[] = "    username: {$password}";
        $config[] = "    password: {$password}";
        
        return implode("\n", $config) . "\n";
    }

    public static function buildMieru($password, $server)
    {
        $protocol_settings = data_get($server, 'protocol_settings', []);
        $config = [];
        $config[] = "- mieru:";
        $config[] = "    name: \"{$server['name']}\"";
        $config[] = "    server: {$server['host']}";
        $config[] = "    port: {$server['port']}";
        $config[] = "    username: {$password}";
        $config[] = "    password: {$password}";
        $config[] = "    transport: " . strtolower(data_get($protocol_settings, 'transport', 'TCP'));
        
        return implode("\n", $config) . "\n";
    }
}
