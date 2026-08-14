const syntheticDNSTranslations = {
  en: {
    "允许 Provider 使用 Synthetic DNS / Fake-IP": "Allow Providers to Use Synthetic DNS / Fake-IP",
    "仅当 TokenHub 所在主机启用了 Fake-IP 代理，且 Provider 域名因此解析到合成地址时开启。该例外只用于域名解析结果，不允许把字面量 IP 直接配置为 Provider Base URL。": "Enable only when the TokenHub host uses a Fake-IP proxy and provider domains therefore resolve to synthetic addresses. This exception applies only to DNS results; a literal IP still cannot be used as a Provider Base URL.",
    "Synthetic DNS / Fake-IP 网段": "Synthetic DNS / Fake-IP Ranges",
    "安全提示：每行一个 CIDR，必须与本机代理的实际 Fake-IP 池一致。198.18.0.0/15 是基准测试保留网段，只是常被代理软件用作 Fake-IP，并非 Fake-IP 专属；不同软件或配置可使用其他网段。配置过宽或填入真实内网会削弱 SSRF 防护。loopback、link-local、metadata、multicast、NAT64 等敏感范围始终禁止。": "Security notice: enter one CIDR per line and match the Fake-IP pool actually used by the local proxy. 198.18.0.0/15 is reserved for benchmarking and is merely common for Fake-IP; it is not exclusive to Fake-IP, and other software or configurations may use different ranges. Overly broad ranges or real internal networks weaken SSRF protection. Sensitive loopback, link-local, metadata, multicast, and NAT64 ranges always remain blocked.",
    "开启时至少填写一个 Synthetic DNS CIDR。": "Enter at least one Synthetic DNS CIDR when the exception is enabled.",
    "Synthetic DNS CIDR 格式无效，请每行填写一个 CIDR。": "The Synthetic DNS CIDR format is invalid. Enter one CIDR per line.",
    "Synthetic DNS CIDR 范围过宽，请缩小后重试。": "The Synthetic DNS CIDR is too broad. Narrow the range and try again.",
    "该 Synthetic DNS CIDR 与受保护地址范围重叠，无法保存。": "This Synthetic DNS CIDR overlaps a protected address range and cannot be saved.",
  },
  ja: {
    "允许 Provider 使用 Synthetic DNS / Fake-IP": "Provider による Synthetic DNS / Fake-IP の使用を許可",
    "仅当 TokenHub 所在主机启用了 Fake-IP 代理，且 Provider 域名因此解析到合成地址时开启。该例外只用于域名解析结果，不允许把字面量 IP 直接配置为 Provider Base URL。": "TokenHub ホストで Fake-IP プロキシを使用し、Provider ドメインが合成アドレスに解決される場合にのみ有効にしてください。この例外は DNS の解決結果だけに適用され、リテラル IP を Provider Base URL に直接指定することはできません。",
    "Synthetic DNS / Fake-IP 网段": "Synthetic DNS / Fake-IP 範囲",
    "安全提示：每行一个 CIDR，必须与本机代理的实际 Fake-IP 池一致。198.18.0.0/15 是基准测试保留网段，只是常被代理软件用作 Fake-IP，并非 Fake-IP 专属；不同软件或配置可使用其他网段。配置过宽或填入真实内网会削弱 SSRF 防护。loopback、link-local、metadata、multicast、NAT64 等敏感范围始终禁止。": "セキュリティ上の注意：1 行に 1 つの CIDR を入力し、ローカルプロキシが実際に使用する Fake-IP プールと一致させてください。198.18.0.0/15 はベンチマーク用の予約範囲であり、Fake-IP でよく使われるだけで専用ではありません。ソフトウェアや設定によっては別の範囲が使われます。広すぎる範囲や実在する内部ネットワークを指定すると SSRF 防御が弱まります。loopback、link-local、metadata、multicast、NAT64 などの機密範囲は常にブロックされます。",
    "开启时至少填写一个 Synthetic DNS CIDR。": "この例外を有効にする場合は、Synthetic DNS CIDR を 1 つ以上入力してください。",
    "Synthetic DNS CIDR 格式无效，请每行填写一个 CIDR。": "Synthetic DNS CIDR の形式が正しくありません。1 行に 1 つの CIDR を入力してください。",
    "Synthetic DNS CIDR 范围过宽，请缩小后重试。": "Synthetic DNS CIDR の範囲が広すぎます。範囲を狭めて再試行してください。",
    "该 Synthetic DNS CIDR 与受保护地址范围重叠，无法保存。": "この Synthetic DNS CIDR は保護対象のアドレス範囲と重複しているため保存できません。",
  },
};

export default syntheticDNSTranslations;
