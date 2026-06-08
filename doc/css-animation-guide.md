# CSS 动画 + Tailwind 实现方案

> 适用于：科技感暗色网站、产品功能展示页、数据流可视化
> 依赖：Tailwind CSS（CDN 或项目安装均可），无 JS 动画库

---

## 技术架构

```
┌─────────────────────────────────────────────────┐
│  Tailwind CSS                                    │
│  • 布局/间距/颜色/响应式                          │
│  • hover/group-hover 交互状态                     │
│  • transition-* 过渡效果                          │
└──────────────────────┬──────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────┐
│  自定义 @keyframes                               │
│  • 流动、发光、移动、旋转等连续动画               │
│  • 通过 animation-delay 编排多元素时序            │
└──────────────────────┬──────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────┐
│  SVG                                             │
│  • 连接线路径                                    │
│  • stroke-dasharray 配合 CSS 做流动效果          │
│  • 图标（Lucide / Heroicons）                    │
└─────────────────────────────────────────────────┘
```

---

## 在项目中引入

### 方式 A：CDN（快速原型/单页面）

```html
<script src="https://cdn.tailwindcss.com"></script>
```

### 方式 B：npm 安装（正式项目）

```bash
npm install -D tailwindcss postcss autoprefixer
npx tailwindcss init -p
```

```js
// tailwind.config.js
module.exports = {
  content: ['./src/**/*.{html,js,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      // 自定义动画扩展（下面会详细说明）
      animation: {
        'dash-flow': 'dash-flow 1.5s linear infinite',
        'pulse-glow': 'pulse-glow 2.5s ease-in-out infinite',
        'dot-move': 'dot-move 2s ease-in-out infinite',
        'fade-in-up': 'fade-in-up 0.8s ease-out forwards',
        'spin-slow': 'spin-slow 8s linear infinite',
        'shimmer': 'shimmer 3s ease-in-out infinite',
      },
      keyframes: {
        'dash-flow': {
          to: { 'stroke-dashoffset': '-20' }
        },
        'pulse-glow': {
          '0%, 100%': { 'box-shadow': '0 0 8px rgba(0,255,170,0.3), 0 0 16px rgba(0,255,170,0.1)' },
          '50%': { 'box-shadow': '0 0 16px rgba(0,255,170,0.6), 0 0 32px rgba(0,255,170,0.3)' }
        },
        'dot-move': {
          '0%': { transform: 'translateX(0)', opacity: '0' },
          '10%': { opacity: '1' },
          '90%': { opacity: '1' },
          '100%': { transform: 'translateX(160px)', opacity: '0' }
        },
        'fade-in-up': {
          from: { opacity: '0', transform: 'translateY(20px)' },
          to: { opacity: '1', transform: 'translateY(0)' }
        },
        'spin-slow': {
          from: { transform: 'rotate(0deg)' },
          to: { transform: 'rotate(360deg)' }
        },
        'shimmer': {
          '0%': { 'background-position': '-200% center' },
          '100%': { 'background-position': '200% center' }
        }
      }
    }
  }
}
```

---

## 动效清单（可复用）

### 1. 虚线流动

**效果**：SVG 路径上的虚线像水流一样向前移动

**原理**：`stroke-dasharray` 创建虚线，`stroke-dashoffset` 负方向偏移产生流动感

```css
@keyframes dash-flow {
    to { stroke-dashoffset: -20; }
}
.animate-dash-flow {
    animation: dash-flow 1.5s linear infinite;
}
```

```html
<svg viewBox="0 0 400 100" fill="none">
    <path 
        d="M 0 50 H 400" 
        stroke="rgba(0,255,170,0.4)" 
        stroke-width="1.5" 
        stroke-dasharray="6 4" 
        class="animate-dash-flow"
    />
</svg>
```

**参数调整**：
- `stroke-dasharray="6 4"` → 虚线段长6，间隔4（改大更稀疏）
- `stroke-dashoffset: -20` → 偏移量（与 dasharray 总长匹配效果最好）
- `1.5s` → 速度（越小越快）

---

### 2. 脉冲发光

**效果**：元素边缘有呼吸式的光晕，强弱循环

**原理**：`box-shadow` 在两个强度之间切换

```css
@keyframes pulse-glow {
    0%, 100% { 
        box-shadow: 0 0 8px rgba(0,255,170,0.3),
                    0 0 16px rgba(0,255,170,0.1); 
    }
    50% { 
        box-shadow: 0 0 16px rgba(0,255,170,0.6),
                    0 0 32px rgba(0,255,170,0.3); 
    }
}
.animate-pulse-glow {
    animation: pulse-glow 2.5s ease-in-out infinite;
}
```

```html
<div class="w-16 h-16 rounded-xl border-2 border-emerald-500/50 animate-pulse-glow">
    <!-- 内容 -->
</div>
```

**参数调整**：
- 颜色 `rgba(0,255,170,...)` → 改成你项目的主色
- `2.5s` → 呼吸节奏（越大越慢越柔和）
- 多层 shadow → 增加外层扩散范围

---

### 3. 移动粒子（小点沿路径）

**效果**：小圆点从左到右移动，多个点错开时间

**原理**：`translateX` 位移 + `opacity` 淡入淡出 + `animation-delay`

```css
@keyframes dot-move {
    0%   { transform: translateX(0);     opacity: 0; }
    10%  { opacity: 1; }
    90%  { opacity: 1; }
    100% { transform: translateX(160px); opacity: 0; }
}
.animate-dot-move { animation: dot-move 2s ease-in-out infinite; }
```

```html
<!-- 多个点通过 animation-delay 错开 -->
<div class="w-2 h-2 rounded-full bg-emerald-400 animate-dot-move"></div>
<div class="w-2 h-2 rounded-full bg-emerald-400 animate-dot-move" style="animation-delay: 0.7s"></div>
<div class="w-2 h-2 rounded-full bg-emerald-400 animate-dot-move" style="animation-delay: 1.4s"></div>
```

**参数调整**：
- `translateX(160px)` → 移动距离，匹配容器宽度
- `animation-delay` → 多个点的间隔时间
- 垂直移动改用 `translateY`

---

### 4. 淡入上浮

**效果**：元素从下方 20px 处淡入到正常位置，依次出现

**原理**：`opacity` + `translateY` + `animation-delay` 递增

```css
@keyframes fade-in-up {
    from { opacity: 0; transform: translateY(20px); }
    to   { opacity: 1; transform: translateY(0); }
}
.animate-fade-in-up {
    animation: fade-in-up 0.8s ease-out forwards;
    opacity: 0; /* 初始不可见 */
}
```

```html
<div class="animate-fade-in-up">第一个</div>
<div class="animate-fade-in-up" style="animation-delay: 0.2s">第二个</div>
<div class="animate-fade-in-up" style="animation-delay: 0.4s">第三个</div>
```

**注意**：`forwards` 关键字让动画结束后保持最终状态（不回弹到 opacity:0）

---

### 5. 悬停卡片交互

**效果**：鼠标移入时卡片上浮、边框变亮、出现渐变背景

**原理**：Tailwind 的 `group` + `group-hover` + `transition`

```html
<div class="group relative bg-gray-900 border border-gray-800 rounded-xl p-6
            transition-all duration-300
            hover:border-emerald-700 
            hover:shadow-lg hover:shadow-emerald-900/20 
            hover:-translate-y-1">
    
    <!-- 渐变遮罩层（悬停时显现） -->
    <div class="absolute inset-0 rounded-xl 
                bg-gradient-to-b from-emerald-500/5 to-transparent 
                opacity-0 group-hover:opacity-100 
                transition-opacity duration-300">
    </div>
    
    <!-- 内容 -->
    <div class="relative z-10">
        <h3 class="text-white">标题</h3>
        <p class="text-gray-400">描述文字</p>
    </div>
</div>
```

**关键点**：
- `group` 放在父容器，`group-hover:*` 放在子元素
- `transition-all duration-300` 让所有属性变化平滑过渡
- `hover:-translate-y-1` 让卡片上浮 4px
- 渐变遮罩用 `absolute inset-0` 覆盖整个卡片

---

### 6. 光线扫过（Shimmer）

**效果**：卡片/区域有一道微光从左到右扫过

**原理**：`linear-gradient` 背景 + `background-position` 动画

```css
@keyframes shimmer {
    0%   { background-position: -200% center; }
    100% { background-position: 200% center; }
}
.animate-shimmer {
    background: linear-gradient(
        90deg, 
        transparent 0%, 
        rgba(0, 255, 170, 0.08) 50%, 
        transparent 100%
    );
    background-size: 200% 100%;
    animation: shimmer 3s ease-in-out infinite;
}
```

```html
<div class="bg-gray-900 rounded-xl p-8 animate-shimmer">
    <!-- 内容 -->
</div>
```

---

### 7. 慢速旋转

**效果**：图标/齿轮缓慢持续旋转

```css
@keyframes spin-slow {
    from { transform: rotate(0deg); }
    to   { transform: rotate(360deg); }
}
.animate-spin-slow {
    animation: spin-slow 8s linear infinite;
}
```

```html
<svg class="w-8 h-8 text-emerald-400 animate-spin-slow">
    <!-- 齿轮/圆形图标 SVG -->
</svg>
```

---

## 设计规范

### 色彩系统

| 用途 | 色值 | Tailwind 类 |
|------|------|-------------|
| 背景 | #030712 | `bg-gray-950` |
| 卡片背景 | #111827 | `bg-gray-900` |
| 边框常态 | #1f2937 | `border-gray-800` |
| 边框高亮 | emerald-700 | `border-emerald-700` |
| 主色（发光/强调） | #00ffaa | `text-emerald-400` |
| 正文 | #9ca3af | `text-gray-400` |
| 标题 | #ffffff | `text-white` |

### 动画时间规范

| 场景 | 推荐时长 | easing |
|------|----------|--------|
| 悬停过渡 | 200-300ms | ease-out |
| 淡入出现 | 600-800ms | ease-out |
| 连续循环（发光） | 2-3s | ease-in-out |
| 连续循环（流动） | 1-2s | linear |
| 慢速旋转 | 6-10s | linear |

### 性能守则

1. **只动画 `transform` 和 `opacity`** — 这两个属性由 GPU 合成层处理，不触发重排
2. **避免动画 `width/height/margin/padding`** — 会触发回流，性能差
3. **`box-shadow` 动画用于小面积** — 大面积 shadow 动画在低端机上可能卡顿
4. **`will-change` 慎用** — 不要全局加，只在确实需要的元素上加
5. **`prefers-reduced-motion` 媒体查询** — 为不想看动画的用户提供选项：

```css
@media (prefers-reduced-motion: reduce) {
    *, *::before, *::after {
        animation-duration: 0.01ms !important;
        transition-duration: 0.01ms !important;
    }
}
```

---

## 在 React/Vue 项目中使用

### React + Tailwind

```jsx
// components/FlowLine.tsx
export function FlowLine({ width = 200 }) {
  return (
    <svg width={width} height="4" viewBox={`0 0 ${width} 4`} fill="none">
      <path
        d={`M 0 2 H ${width}`}
        stroke="rgba(0,255,170,0.4)"
        strokeWidth="1.5"
        strokeDasharray="6 4"
        className="animate-dash-flow"
      />
    </svg>
  );
}
```

```jsx
// components/GlowNode.tsx
export function GlowNode({ children }) {
  return (
    <div className="w-16 h-16 rounded-xl border-2 border-emerald-500/50 
                    bg-gray-900 flex items-center justify-center animate-pulse-glow">
      {children}
    </div>
  );
}
```

### Vue + Tailwind

```vue
<!-- components/FlowDot.vue -->
<template>
  <div 
    class="w-2 h-2 rounded-full bg-emerald-400 animate-dot-move"
    :style="{ animationDelay: `${delay}s` }"
  />
</template>

<script setup>
defineProps({ delay: { type: Number, default: 0 } })
</script>
```

---

## 文件清单

| 文件 | 用途 |
|------|------|
| `demo-animation.html` | 完整可运行的 Demo（浏览器直接打开） |
| `css-animation-guide.md` | 本文档（技术方案 + 复用指南） |

---

## 快速复制模板

如果你在新项目中要快速用上这套动效，最小集合是：

```html
<!-- 1. 引入 Tailwind -->
<script src="https://cdn.tailwindcss.com"></script>

<!-- 2. 在 <style> 中加入这段自定义动画 -->
<style>
@keyframes dash-flow { to { stroke-dashoffset: -20; } }
@keyframes pulse-glow {
    0%, 100% { box-shadow: 0 0 8px rgba(0,255,170,0.3); }
    50% { box-shadow: 0 0 20px rgba(0,255,170,0.6); }
}
@keyframes fade-in-up {
    from { opacity: 0; transform: translateY(20px); }
    to { opacity: 1; transform: translateY(0); }
}
.animate-dash-flow { animation: dash-flow 1.5s linear infinite; }
.animate-pulse-glow { animation: pulse-glow 2.5s ease-in-out infinite; }
.animate-fade-in-up { animation: fade-in-up 0.8s ease-out forwards; opacity: 0; }
</style>

<!-- 3. 开始使用 class -->
<body class="bg-gray-950 text-white">
    <!-- 你的内容 -->
</body>
```

以上就够覆盖 80% 的科技感动效需求了。
