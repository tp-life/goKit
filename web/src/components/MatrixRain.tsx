import { useEffect, useRef } from 'react';

// 片假名 + 数字，经典矩阵字符集
const CHARS = 'アイウエオカキクケコサシスセソタチツテトナニヌネノ0123456789'.split('');
const FONT_SIZE = 14;
const FRAME_INTERVAL = 55; // ms，控制下落速度

/** #RRGGBB → rgba(r, g, b, alpha) */
function hexToRgba(hex: string, alpha: number): string {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return hex;
  const n = parseInt(m[1], 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}

interface MatrixRainProps {
  className?: string;
  /** 字符主体色，默认矩阵绿 */
  color?: string;
  /** 少量高亮"头部"字符色 */
  headColor?: string;
  /** 画布底色（用于渐隐尾迹），需与页面背景一致 */
  bg?: string;
}

/** 矩阵字符雨背景（Canvas 自绘，卸载时 cancelAnimationFrame 清理） */
export default function MatrixRain({
  className,
  color = '#00FF41',
  headColor = '#B3FFBD',
  bg = '#0D0208',
}: MatrixRainProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    let rafId = 0;
    let last = 0;
    let columns = 0;
    let drops: number[] = [];

    const resize = () => {
      canvas.width = canvas.clientWidth;
      canvas.height = canvas.clientHeight;
      columns = Math.max(1, Math.floor(canvas.width / FONT_SIZE));
      drops = Array.from(
        { length: columns },
        () => Math.floor(Math.random() * (canvas.height / FONT_SIZE)),
      );
      ctx.fillStyle = bg;
      ctx.fillRect(0, 0, canvas.width, canvas.height);
    };

    resize();
    const observer = new ResizeObserver(resize);
    observer.observe(canvas);

    const trail = hexToRgba(bg, 0.12);

    const draw = (t: number) => {
      rafId = requestAnimationFrame(draw);
      if (t - last < FRAME_INTERVAL) return;
      last = t;

      // 半透明覆盖形成渐隐尾迹
      ctx.fillStyle = trail;
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      ctx.font = `${FONT_SIZE}px 'JetBrains Mono', monospace`;

      for (let i = 0; i < columns; i++) {
        const ch = CHARS[(Math.random() * CHARS.length) | 0];
        const x = i * FONT_SIZE;
        const y = drops[i] * FONT_SIZE;
        // 少量字符用亮色作为"头部"高亮
        ctx.fillStyle = Math.random() > 0.975 ? headColor : color;
        ctx.fillText(ch, x, y);
        if (y > canvas.height && Math.random() > 0.975) drops[i] = 0;
        drops[i] += 1;
      }
    };

    rafId = requestAnimationFrame(draw);
    return () => {
      cancelAnimationFrame(rafId);
      observer.disconnect();
    };
  }, [color, headColor, bg]);

  return <canvas ref={canvasRef} className={className} aria-hidden="true" />;
}
