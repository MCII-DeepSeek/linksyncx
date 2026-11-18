export default {
  batchImageGuide: {
    title: '图片批量生成',
    description: '支持一次性批量提交多组提示词，任务完成后一键打包下载全部生成图像'
  },
  // Home Page
  home: {
    viewOnGithub: '在 GitHub 上查看',
    viewDocs: '查看文档',
    docs: '文档',
    switchToLight: '切换到浅色模式',
    switchToDark: '切换到深色模式',
    dashboard: '控制台',
    login: '登录',
    getStarted: '立即开始',
    goToDashboard: '进入控制台',
    // 新增：面向用户的价值主张
    heroSubtitle: '一枚密钥，畅联全球前沿大模型',
    heroDescription: '告别繁琐的多平台订阅与密钥管理，一站式无缝接入 Claude、OpenAI、Gemini、Antigravity、Grok 等主流 AI 服务',
    tags: {
      subscriptionToApi: '订阅转标准 API',
      stickySession: '智能会话保持',
      realtimeBilling: 'Token 级按量计费'
    },
    // 用户痛点区块
    painPoints: {
      title: '多平台 AI 接入，是否正面临这些困扰？',
      items: {
        expensive: {
          title: '固定订阅成本高昂',
          desc: '各家模型需分别开通月度订阅，额度闲置浪费，综合开支居高不下'
        },
        complex: {
          title: '多源凭证管理繁杂',
          desc: '不同厂商的账号、协议与 API Key 分散各处，开发接入与维护成本高'
        },
        unstable: {
          title: '单号并发易触发限流',
          desc: '单一订阅账号频繁遭遇速率限制与额度触顶，核心工作流频频中断'
        },
        noControl: {
          title: '团队用量缺乏管控',
          desc: '难以追踪各模型与成员的真实消耗，缺乏细粒度的预算与速率边界'
        }
      }
    },
    // 解决方案区块
    solutions: {
      title: '一站式企业级 AI 网关方案',
      subtitle: '简单三步，开启稳定、高效、可控的模型调用体验'
    },
    features: {
      unifiedGateway: '统一入口，即插即用',
      unifiedGatewayDesc: '凭一枚标准 API Key 即可跨协议调用全平台前沿模型，无缝兼容 Claude Code、Codex、Gemini CLI 等主流开发工具。',
      multiAccount: '集群调度，稳如磐石',
      multiAccountDesc: '智能编排多账号池，支持粘性会话保持、负载均衡与毫秒级自动故障切换，从容应对高并发生产流量。',
      balanceQuota: '精细计量，透明可控',
      balanceQuotaDesc: '精确至 Token 级的实时账单与缓存核算，支持灵活设定配额上限、速率规则与模型白名单，团队支出清晰可查。'
    },
    // 优势对比
    comparison: {
      title: '为什么选择我们？',
      headers: {
        feature: '对比维度',
        official: '传统官方订阅',
        us: '本网关平台'
      },
      items: {
        pricing: {
          feature: '计费模式',
          official: '固定包月，额度闲置仍需付费',
          us: '按实际用量计费，灵活经济'
        },
        models: {
          feature: '模型生态',
          official: '绑定单一厂商生态',
          us: '主流前沿模型自由切换'
        },
        management: {
          feature: '凭证管理',
          official: '多平台账号与密钥各自维护',
          us: '一枚密钥统一鉴权，集中管控'
        },
        stability: {
          feature: '高可用保障',
          official: '单账号易触发频率与额度限制',
          us: '号池智能路由，故障无感切换'
        },
        control: {
          feature: '预算与风控',
          official: '缺乏细粒度配额与审计能力',
          us: '多维限额管控，逐笔调用可溯'
        }
      }
    },
    providers: {
      title: '全栈覆盖主流 AI 模型生态',
      description: '统一标准接口，自由连接全球顶尖模型能力',
      supported: '已支持',
      soon: '持续扩展',
      claude: 'Claude',
      gemini: 'Gemini',
      antigravity: 'Antigravity',
      more: '更多生态'
    },
    // CTA 区块
    cta: {
      title: '即刻开启高效 AI 接入体验',
      description: '快速创建账户，使用统一 API Key 畅享稳定、低延迟的多模型网关服务',
      button: '免费注册'
    },
    footer: {
      allRightsReserved: '保留所有权利。'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key 用量查询',
    subtitle: '输入您的 API Key，实时洞察额度余量、Token 消耗与逐日消费明细',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: '查询',
    querying: '查询中...',
    privacyNote: '安全承诺：您的密钥仅在当前浏览器本地发起查询，不会被第三方存储',
    dateRange: '统计范围:',
    dateRangeToday: '今日',
    dateRange7d: '7 天',
    dateRange30d: '30 天',
    dateRange90d: '90 天',
    dateRangeCustom: '自定义',
    apply: '应用',
    used: '已使用',
    detailInfo: '详细信息',
    tokenStats: 'Token 统计',
    dailyDetail: '按日明细',
    modelStats: '模型用量统计',
    // Table headers
    date: '日期',
    model: '模型',
    requests: '请求数',
    inputTokens: '输入 Tokens',
    outputTokens: '输出 Tokens',
    cacheCreationTokens: '缓存创建',
    cacheReadTokens: '缓存读取',
    cacheWriteTokens: '缓存写入',
    totalTokens: '总 Tokens',
    cost: '费用',
    // Status
    quotaMode: 'Key 限额模式',
    walletBalance: '钱包余额',
    // Ring card titles
    totalQuota: '总额度',
    limit5h: '5 小时限额',
    limitDaily: '日限额',
    limit7d: '7 天限额',
    limitWeekly: '周限额',
    limitMonthly: '月限额',
    // Detail rows
    remainingQuota: '剩余额度',
    expiresAt: '过期时间',
    todayExpires: '(今日到期)',
    daysLeft: '({days} 天)',
    usedQuota: '已用额度',
    resetNow: '即将重置',
    subscriptionType: '订阅类型',
    billingType: '计费方式',
    subscriptionExpires: '订阅到期',
    // Usage stat cells
    todayRequests: '今日请求',
    todayInputTokens: '今日输入',
    todayOutputTokens: '今日输出',
    todayTokens: '今日 Tokens',
    todayCacheCreation: '今日缓存创建',
    todayCacheRead: '今日缓存读取',
    todayCost: '今日费用',
    rpmTpm: 'RPM / TPM',
    totalRequests: '累计请求',
    totalInputTokens: '累计输入',
    totalOutputTokens: '累计输出',
    totalTokensLabel: '累计 Tokens',
    totalCacheCreation: '累计缓存创建',
    totalCacheRead: '累计缓存读取',
    totalCost: '累计费用',
    avgDuration: '平均耗时',
    // Messages
    enterApiKey: '请输入 API Key',
    querySuccess: '查询成功',
    queryFailed: '查询失败',
    queryFailedRetry: '查询失败，请稍后重试',
    noDailyUsage: '暂无按日用量数据',
  },

  // Setup Wizard
  setup: {
    title: 'Sub2API 安装向导',
    description: '配置您的 Sub2API 实例',
    database: {
      title: '数据库配置',
      description: '连接到您的 PostgreSQL 数据库',
      host: '主机',
      port: '端口',
      username: '用户名',
      password: '密码',
      databaseName: '数据库名称',
      sslMode: 'SSL 模式',
      passwordPlaceholder: '密码',
      ssl: {
        disable: '禁用',
        require: '要求',
        verifyCa: '验证 CA',
        verifyFull: '完全验证'
      }
    },
    redis: {
      title: 'Redis 配置',
      description: '连接到您的 Redis 服务器',
      host: '主机',
      port: '端口',
      username: '用户名（可选）',
      password: '密码（可选）',
      database: '数据库',
      usernamePlaceholder: '默认用户留空',
      passwordPlaceholder: '密码',
      enableTls: '启用 TLS',
      enableTlsHint: '连接 Redis 时使用 TLS（公共 CA 证书）'
    },
    admin: {
      title: '管理员账户',
      description: '创建您的管理员账户',
      email: '邮箱',
      password: '密码',
      confirmPassword: '确认密码',
      passwordPlaceholder: '至少 8 个字符',
      confirmPasswordPlaceholder: '确认密码',
      passwordMismatch: '密码不匹配'
    },
    ready: {
      title: '准备安装',
      description: '检查您的配置并完成安装',
      database: '数据库',
      redis: 'Redis',
      adminEmail: '管理员邮箱'
    },
    status: {
      testing: '测试中...',
      success: '连接成功',
      testConnection: '测试连接',
      installing: '安装中...',
      completeInstallation: '完成安装',
      completed: '安装完成！',
      redirecting: '正在跳转到登录页面...',
      restarting: '服务正在重启，请稍候...',
      timeout: '服务重启时间超出预期，请手动刷新页面。'
    }
  },

  // Common
}
