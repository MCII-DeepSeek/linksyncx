export default {
  batchImageGuide: {
    title: 'Batch Image Generation',
    description: 'Submit multiple prompts in a single batch job and download all generated images in one click'
  },
  // Home Page
  home: {
    viewOnGithub: 'View on GitHub',
    viewDocs: 'View Documentation',
    docs: 'Docs',
    switchToLight: 'Switch to Light Mode',
    switchToDark: 'Switch to Dark Mode',
    dashboard: 'Dashboard',
    login: 'Login',
    getStarted: 'Get Started',
    goToDashboard: 'Go to Dashboard',
    // User-focused value proposition
    heroSubtitle: 'One Unified Key for Frontier AI Models',
    heroDescription: 'Eliminate fragmented subscriptions and scattered credentials. Connect to Claude, OpenAI, Gemini, Antigravity, Grok, and more through one unified gateway',
    tags: {
      subscriptionToApi: 'Subscription to Standard API',
      stickySession: 'Smart Session Affinity',
      realtimeBilling: 'Token-Accurate Billing'
    },
    // Pain points section
    painPoints: {
      title: 'Scaling Across AI Providers Shouldn’t Be Hard',
      items: {
        expensive: {
          title: 'Idle Subscription Spend',
          desc: 'Paying fixed monthly fees across multiple providers even when quotas sit unused'
        },
        complex: {
          title: 'Fragmented Credentials',
          desc: 'Juggling disparate accounts, SDKs, and API keys across incompatible vendor endpoints'
        },
        unstable: {
          title: 'Single-Account Bottlenecks',
          desc: 'Individual accounts hit rate limits or cooldown windows, stalling critical workflows'
        },
        noControl: {
          title: 'Limited Cost Visibility',
          desc: 'Lack of granular token attribution, rate guardrails, and per-member quota controls'
        }
      }
    },
    // Solutions section
    solutions: {
      title: 'Enterprise-Ready Unified AI Gateway',
      subtitle: 'Three streamlined pillars for reliable, cost-controlled model access'
    },
    features: {
      unifiedGateway: 'Drop-In Unified Endpoint',
      unifiedGatewayDesc: 'Issue a single standard API Key to invoke frontier models across protocols—natively compatible with Claude Code, Codex, and Gemini CLI.',
      multiAccount: 'Resilient Pool Orchestration',
      multiAccountDesc: 'Priority- and health-aware load balancing with sticky sessions and automatic failover keeps high-concurrency workloads running smoothly.',
      balanceQuota: 'Granular Metering & Guardrails',
      balanceQuotaDesc: 'Real-time token and cache accounting with configurable spend caps, rate limits, and model allowlists for complete team visibility.'
    },
    // Comparison section
    comparison: {
      title: 'Why Choose Our Gateway?',
      headers: {
        feature: 'Dimension',
        official: 'Direct Subscriptions',
        us: 'Unified Gateway'
      },
      items: {
        pricing: {
          feature: 'Billing Model',
          official: 'Fixed monthly fee regardless of usage',
          us: 'Pay-as-you-go with exact token metering'
        },
        models: {
          feature: 'Model Ecosystem',
          official: 'Locked into a single vendor',
          us: 'Switch seamlessly across frontier providers'
        },
        management: {
          feature: 'Credential Governance',
          official: 'Separate keys and consoles per service',
          us: 'Single API key with centralized governance'
        },
        stability: {
          feature: 'High Availability',
          official: 'Vulnerable to single-account rate limits',
          us: 'Multi-account pooling with instant failover'
        },
        control: {
          feature: 'Cost & Risk Control',
          official: 'Coarse or unavailable team limits',
          us: 'Fine-grained quotas & request-level audit logs'
        }
      }
    },
    providers: {
      title: 'Comprehensive Frontier Model Coverage',
      description: 'One standardized interface, connected to leading AI ecosystems',
      supported: 'Supported',
      soon: 'Expanding',
      claude: 'Claude',
      gemini: 'Gemini',
      antigravity: 'Antigravity',
      more: 'More Providers'
    },
    // CTA section
    cta: {
      title: 'Ready to Streamline Your AI Stack?',
      description: 'Create an account in seconds and experience resilient, low-latency multi-model access with a single API Key',
      button: 'Sign Up Free'
    },
    footer: {
      allRightsReserved: 'All rights reserved.'
    }
  },

  // Key Usage Query Page
  keyUsage: {
    title: 'API Key Usage',
    subtitle: 'Enter your API Key to inspect real-time quota balance, token consumption, and daily cost breakdowns',
    placeholder: 'sk-ant-mirror-xxxxxxxxxxxx',
    query: 'Query',
    querying: 'Querying...',
    privacyNote: 'Privacy Notice: Your API Key is queried directly from your browser and is never stored by third parties',
    dateRange: 'Date Range:',
    dateRangeToday: 'Today',
    dateRange7d: '7 Days',
    dateRange30d: '30 Days',
    dateRange90d: '90 Days',
    dateRangeCustom: 'Custom',
    apply: 'Apply',
    used: 'Used',
    detailInfo: 'Detail Information',
    tokenStats: 'Token Statistics',
    dailyDetail: 'Daily Detail',
    modelStats: 'Model Usage Statistics',
    // Table headers
    date: 'Date',
    model: 'Model',
    requests: 'Requests',
    inputTokens: 'Input Tokens',
    outputTokens: 'Output Tokens',
    cacheCreationTokens: 'Cache Creation',
    cacheReadTokens: 'Cache Read',
    cacheWriteTokens: 'Cache Write',
    totalTokens: 'Total Tokens',
    cost: 'Cost',
    // Status
    quotaMode: 'Key Quota Mode',
    walletBalance: 'Wallet Balance',
    // Ring card titles
    totalQuota: 'Total Quota',
    limit5h: '5-Hour Limit',
    limitDaily: 'Daily Limit',
    limit7d: '7-Day Limit',
    limitWeekly: 'Weekly Limit',
    limitMonthly: 'Monthly Limit',
    // Detail rows
    remainingQuota: 'Remaining Quota',
    expiresAt: 'Expires At',
    todayExpires: '(expires today)',
    daysLeft: '({days} days)',
    usedQuota: 'Used Quota',
    resetNow: 'Resetting soon',
    subscriptionType: 'Subscription Type',
    billingType: 'Billing Type',
    subscriptionExpires: 'Subscription Expires',
    // Usage stat cells
    todayRequests: 'Today Requests',
    todayInputTokens: 'Today Input',
    todayOutputTokens: 'Today Output',
    todayTokens: 'Today Tokens',
    todayCacheCreation: 'Today Cache Creation',
    todayCacheRead: 'Today Cache Read',
    todayCost: 'Today Cost',
    rpmTpm: 'RPM / TPM',
    totalRequests: 'Total Requests',
    totalInputTokens: 'Total Input',
    totalOutputTokens: 'Total Output',
    totalTokensLabel: 'Total Tokens',
    totalCacheCreation: 'Total Cache Creation',
    totalCacheRead: 'Total Cache Read',
    totalCost: 'Total Cost',
    avgDuration: 'Avg Duration',
    // Messages
    enterApiKey: 'Please enter an API Key',
    querySuccess: 'Query successful',
    queryFailed: 'Query failed',
    queryFailedRetry: 'Query failed, please try again later',
    noDailyUsage: 'No daily usage data',
  },

  // Setup Wizard
  setup: {
    title: 'Sub2API Setup',
    description: 'Configure your Sub2API instance',
    database: {
      title: 'Database Configuration',
      description: 'Connect to your PostgreSQL database',
      host: 'Host',
      port: 'Port',
      username: 'Username',
      password: 'Password',
      databaseName: 'Database Name',
      sslMode: 'SSL Mode',
      passwordPlaceholder: 'Password',
      ssl: {
        disable: 'Disable',
        require: 'Require',
        verifyCa: 'Verify CA',
        verifyFull: 'Verify Full'
      }
    },
    redis: {
      title: 'Redis Configuration',
      description: 'Connect to your Redis server',
      host: 'Host',
      port: 'Port',
      username: 'Username (optional)',
      password: 'Password (optional)',
      database: 'Database',
      usernamePlaceholder: 'Leave empty for default user',
      passwordPlaceholder: 'Password',
      enableTls: 'Enable TLS',
      enableTlsHint: 'Use TLS when connecting to Redis (public CA certs)'
    },
    admin: {
      title: 'Admin Account',
      description: 'Create your administrator account',
      email: 'Email',
      password: 'Password',
      confirmPassword: 'Confirm Password',
      passwordPlaceholder: 'Min 8 characters',
      confirmPasswordPlaceholder: 'Confirm password',
      passwordMismatch: 'Passwords do not match'
    },
    ready: {
      title: 'Ready to Install',
      description: 'Review your configuration and complete setup',
      database: 'Database',
      redis: 'Redis',
      adminEmail: 'Admin Email'
    },
    status: {
      testing: 'Testing...',
      success: 'Connection Successful',
      testConnection: 'Test Connection',
      installing: 'Installing...',
      completeInstallation: 'Complete Installation',
      completed: 'Installation completed!',
      redirecting: 'Redirecting to login page...',
      restarting: 'Service is restarting, please wait...',
      timeout: 'Service restart is taking longer than expected. Please refresh the page manually.'
    }
  },

  // Common
}
