import { theme } from 'antd';
import type { ThemeConfig } from 'antd';
import type { ComponentProps } from 'react';
import type { ProLayout } from '@ant-design/pro-components';

type LayoutToken = NonNullable<ComponentProps<typeof ProLayout>['token']>;

/** 主题调色板：antd token 与 global.css 变量共用同一份色值定义 */
interface Palette {
  dark: boolean;
  primary: string; // 主强调色
  cyan: string; // 链接 / 次强调
  purple: string;
  title: string; // 标题
  text: string; // 正文
  secondary: string; // 次要文字
  bg: string; // 布局底
  card: string; // 卡片 / 容器
  elevated: string; // 浮层
  border: string;
  border2: string;
  onPrimary: string; // 主按钮文字色
  siderBg: string;
  siderText: string;
  siderHoverBg: string;
  siderSelectedBg: string;
  siderSelectedText: string;
  headerBg: string;
  inputBg: string;
  tableHeaderBg: string;
  rowHoverBg: string;
  fontBody: string;
}

const SANS = `-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Noto Sans SC', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif`;
const MONO = `'JetBrains Mono', 'Noto Sans SC', -apple-system, 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', monospace`;

export interface ThemePreset {
  id: string;
  name: string;
  desc: string;
  swatch: string; // 切换器里的色点
  effects: { rain: boolean }; // rain: 数字雨背景
  antd: ThemeConfig;
  layoutToken: LayoutToken;
}

function buildAntd(p: Palette): ThemeConfig {
  return {
    algorithm: p.dark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: p.primary,
      colorInfo: p.cyan,
      colorLink: p.cyan,
      colorLinkHover: p.cyan,
      colorLinkActive: p.cyan,
      colorTextBase: p.text,
      colorText: p.text,
      colorTextSecondary: p.secondary,
      colorTextTertiary: p.secondary,
      colorTextPlaceholder: p.secondary,
      colorBgLayout: p.bg,
      colorBgContainer: p.card,
      colorBgElevated: p.elevated,
      colorBorder: p.border,
      colorBorderSecondary: p.border2,
      colorSuccess: p.primary,
      colorWarning: '#FFD000',
      colorError: '#FF4D4F',
      colorBgSpotlight: p.elevated,
      colorTextLightSolid: p.onPrimary,
      borderRadius: 8,
      fontFamily: p.fontBody,
    },
    components: {
      Layout: {
        siderBg: p.siderBg,
        headerBg: p.headerBg,
        bodyBg: 'transparent',
        headerHeight: 56,
        headerPadding: '0 24px',
      },
      Menu: {
        itemBg: 'transparent',
        itemColor: p.siderText,
        itemHoverBg: p.siderHoverBg,
        itemHoverColor: p.siderSelectedText,
        itemSelectedBg: p.siderSelectedBg,
        itemSelectedColor: p.siderSelectedText,
        itemBorderRadius: 8,
        subMenuItemBg: 'transparent',
      },
      Table: {
        headerBg: p.tableHeaderBg,
        headerColor: p.secondary,
        headerSplitColor: 'transparent',
        borderColor: p.border2,
        rowHoverBg: p.rowHoverBg,
      },
      Card: {
        colorBgContainer: p.card,
      },
      Button: {
        primaryShadow: 'none',
        defaultShadow: 'none',
        dangerShadow: 'none',
      },
      Tag: {
        borderRadiusSM: 999,
      },
      Modal: {
        contentBg: p.card,
        headerBg: p.card,
        borderRadiusLG: 10,
      },
      Input: {
        colorBgContainer: p.inputBg,
        activeBorderColor: p.primary,
      },
      Select: {
        colorBgContainer: p.inputBg,
      },
      Message: {
        contentBg: p.elevated,
      },
      Notification: {
        colorBgElevated: p.elevated,
      },
      Empty: {
        colorTextDescription: p.secondary,
      },
    },
  };
}

function buildLayoutToken(p: Palette): LayoutToken {
  return {
    // ProLayout 会渲染全屏 .ant-pro-layout-bg-list 背景层，默认用 colorBgLayout 不透明填充，
    // 会盖住数字雨 canvas；置透明后由 body 的 var(--bg) 兜底
    bgLayout: 'transparent',
    sider: {
      colorMenuBackground: p.siderBg,
      colorTextMenu: p.siderText,
      colorTextMenuSelected: p.siderSelectedText,
      colorBgMenuItemSelected: p.siderSelectedBg,
      colorTextMenuItemHover: p.siderSelectedText,
      colorBgMenuItemHover: p.siderHoverBg,
    },
    header: {
      colorBgHeader: p.headerBg,
      colorHeaderTitle: p.title,
    },
    pageContainer: {
      paddingInlinePageContainerContent: 28,
      paddingBlockPageContainerContent: 24,
    },
  };
}

function definePreset(
  meta: { id: string; name: string; desc: string; swatch: string; rain?: boolean },
  p: Palette,
): ThemePreset {
  return {
    id: meta.id,
    name: meta.name,
    desc: meta.desc,
    swatch: meta.swatch,
    effects: { rain: meta.rain ?? false },
    antd: buildAntd(p),
    layoutToken: buildLayoutToken(p),
  };
}

/* ---------------- 调色板定义（与 global.css 中 [data-theme] 变量块一一对应） ---------------- */

const matrixPalette: Palette = {
  dark: true,
  primary: '#00FF41',
  cyan: '#00E5FF',
  purple: '#9F00FF',
  title: '#00FF80',
  text: '#B3FFBD',
  secondary: '#88CC99',
  bg: '#0D0208',
  card: '#0F1C0F',
  elevated: '#112211',
  border: '#004D1F',
  border2: '#0A3315',
  onPrimary: '#0D0208',
  siderBg: '#0D0208',
  siderText: '#88CC99',
  siderHoverBg: 'rgba(0, 255, 65, 0.06)',
  siderSelectedBg: 'rgba(0, 255, 65, 0.1)',
  siderSelectedText: '#00FF41',
  headerBg: 'rgba(13, 2, 8, 0.85)',
  inputBg: '#0D0208',
  tableHeaderBg: '#112211',
  rowHoverBg: 'rgba(0, 255, 65, 0.05)',
  fontBody: MONO,
};

const enterprisePalette: Palette = {
  dark: false,
  primary: '#1677FF',
  cyan: '#0958D9',
  purple: '#722ED1',
  title: '#111827',
  text: '#1F2937',
  secondary: '#6B7280',
  bg: '#F3F5F9',
  card: '#FFFFFF',
  elevated: '#FFFFFF',
  border: '#E5E7EB',
  border2: '#EEF1F5',
  onPrimary: '#FFFFFF',
  siderBg: '#001529',
  siderText: 'rgba(255, 255, 255, 0.65)',
  siderHoverBg: 'rgba(255, 255, 255, 0.08)',
  siderSelectedBg: '#1677FF',
  siderSelectedText: '#FFFFFF',
  headerBg: '#FFFFFF',
  inputBg: '#FFFFFF',
  tableHeaderBg: '#FAFBFC',
  rowHoverBg: '#F5F8FF',
  fontBody: SANS,
};

const midnightPalette: Palette = {
  dark: true,
  primary: '#818CF8',
  cyan: '#38BDF8',
  purple: '#A78BFA',
  title: '#F8FAFC',
  text: '#E2E8F0',
  secondary: '#94A3B8',
  bg: '#0F172A',
  card: '#1E293B',
  elevated: '#26334D',
  border: '#334155',
  border2: '#283548',
  onPrimary: '#FFFFFF',
  siderBg: '#0B1120',
  siderText: '#94A3B8',
  siderHoverBg: 'rgba(129, 140, 248, 0.08)',
  siderSelectedBg: 'rgba(129, 140, 248, 0.16)',
  siderSelectedText: '#A5B4FC',
  headerBg: 'rgba(15, 23, 42, 0.85)',
  inputBg: '#0F172A',
  tableHeaderBg: '#26334D',
  rowHoverBg: 'rgba(129, 140, 248, 0.06)',
  fontBody: SANS,
};

const auroraPalette: Palette = {
  dark: true,
  primary: '#C084FC',
  cyan: '#22D3EE',
  purple: '#E879F9',
  title: '#F5F3FF',
  text: '#E9E2F7',
  secondary: '#A89BC4',
  bg: '#120A1F',
  card: '#1C1132',
  elevated: '#251843',
  border: '#3B2560',
  border2: '#2C1B49',
  onPrimary: '#1A0B2E',
  siderBg: '#0D0718',
  siderText: '#A89BC4',
  siderHoverBg: 'rgba(192, 132, 252, 0.08)',
  siderSelectedBg: 'rgba(192, 132, 252, 0.16)',
  siderSelectedText: '#D8B4FE',
  headerBg: 'rgba(18, 10, 31, 0.85)',
  inputBg: '#120A1F',
  tableHeaderBg: '#251843',
  rowHoverBg: 'rgba(192, 132, 252, 0.06)',
  fontBody: SANS,
};

/** 全站可选主题（顺序即切换器中的展示顺序），字符雨颜色取自各主题调色板 */
export const themePresets: ThemePreset[] = [
  definePreset({ id: 'matrix', name: '赛博矩阵', desc: 'Neo-Matrix · 数字雨', swatch: '#00FF41', rain: true }, matrixPalette),
  definePreset({ id: 'enterprise', name: '企业雅蓝', desc: 'Enterprise Light', swatch: '#1677FF', rain: true }, enterprisePalette),
  definePreset({ id: 'midnight', name: '暗夜靛蓝', desc: 'Midnight Indigo', swatch: '#818CF8', rain: true }, midnightPalette),
  definePreset({ id: 'aurora', name: '极光幻紫', desc: 'Aurora Violet', swatch: '#C084FC', rain: true }, auroraPalette),
];

export const defaultThemeId = 'matrix';
