/** 将完整 IPv6 压缩成最短文本 */
/** 将 IPv6 压缩成最短文本 (处理了包含 :: 的不完整输入) */
export function compressIPv6(full: string): string {
  if (!full) return "";

  // --- 新增：标准化处理，把 :: 补全成 0 ---
  let normalized = full;
  if (full.includes("::")) {
    const parts = full.split("::");
    const left = parts[0] ? parts[0].split(":") : [];
    const right = parts[1] ? parts[1].split(":") : [];
    const missingCount = 8 - (left.length + right.length);
    const middle = new Array(missingCount).fill("0");
    normalized = [...left, ...middle, ...right].join(":");
  }

  // 1. 此时 split 出来的每一项都有值了，不会产生 NaN
  const segs = normalized.split(":").map((s) => parseInt(s, 16) || 0);

  // 2. 转换十六进制（这一步已经安全了）
  const zTrim = segs.map((n) => n.toString(16));

  // 3. 找最长连续全 0 段 (保持你原有的逻辑，它是对的)
  let bestStart = -1,
    bestLen = 0;
  for (let i = 0; i < segs.length; ) {
    if (segs[i] === 0) {
      let j = i + 1;
      while (j < segs.length && segs[j] === 0) j++;
      const len = j - i;
      // 只有长度大于 1 的 0 串压缩才有意义
      if (len > bestLen && len > 1) {
        bestLen = len;
        bestStart = i;
      }
      i = j;
    } else i++;
  }

  // 4. 拼接
  const out: string[] = [];
  for (let i = 0; i < segs.length; i++) {
    if (i === bestStart) {
      out.push("");
      i += bestLen - 1;
      if (bestStart === 0 || bestStart + bestLen === 8) {
        // 如果全 0 段在头或尾，补一个空占位保证 join 出来有双冒号
        if (out.length === 1) out.push("");
      }
      continue;
    }
    out.push(zTrim[i]);
  }

  // 5. 合并
  let ans = out.join(":");
  if (ans.includes("::")) {
    // 已经包含双冒号，无需处理
  } else if (bestStart !== -1) {
    ans = ans.replace(/:{2,}/, "::");
  }

  // 处理特殊边缘情况：全 0 地址 "::"
  if (ans === "") return "::";

  return ans.toLowerCase();
}

/* ====== 使用示例 ====== */
// import { compressIPv6 } from '@/utils/ipv6';
// compressIPv6('2001:0db8:0000:0000:0000:ff00:0042:8329')
// → "2001:db8::ff00:42:8329"
