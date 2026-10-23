package js

import "math"

// fdlibmPow is the fdlibm __ieee754_pow algorithm (as used by V8's Math.pow), so that powers round exactly as
// they do in the original JavaScript implementation. Every intermediate product is rounded explicitly.
func fdlibmPow(x, y float64) float64 {
	const (
		bp0    = 1.0
		bp1    = 1.5
		dp0    = 0.0
		dp1    = 5.84962487220764160156e-01
		dl0    = 0.0
		dl1    = 1.35003920212974897128e-08
		two53  = 9007199254740992.0
		huge   = 1.0e300
		tiny   = 1.0e-300
		L1     = 5.99999999999994648725e-01
		L2     = 4.28571428578550184252e-01
		L3     = 3.33333329818377432918e-01
		L4     = 2.72728123808534006489e-01
		L5     = 2.30660745775561754067e-01
		L6     = 2.06975017800338417784e-01
		P1     = 1.66666666666666019037e-01
		P2     = -2.77777777770155933842e-03
		P3     = 6.61375632143793436117e-05
		P4     = -1.65339022054652515390e-06
		P5     = 4.13813679705723846039e-08
		lg2    = 6.93147180559945286227e-01
		lg2H   = 6.93147182464599609375e-01
		lg2L   = -1.90465429995776804525e-09
		ovt    = 8.0085662595372944372e-17
		cp     = 9.61796693925975554329e-01
		cpH    = 9.61796700954437255859e-01
		cpL    = -7.02846165095275826516e-09
		ivln2  = 1.44269504088896338700e+00
		ivln2H = 1.44269502162933349609e+00
		ivln2L = 1.92596299112661746887e-08
	)
	hi := func(v float64) int32 { return int32(math.Float64bits(v) >> 32) }
	lo := func(v float64) uint32 { return uint32(math.Float64bits(v)) }
	withHi := func(v float64, h int32) float64 {
		return math.Float64frombits(uint64(uint32(h))<<32 | uint64(lo(v)))
	}
	fromWords := func(h int32, l uint32) float64 { return math.Float64frombits(uint64(uint32(h))<<32 | uint64(l)) }
	zeroLo := func(v float64) float64 { return math.Float64frombits(math.Float64bits(v) &^ 0xFFFFFFFF) }

	hv, tv := huge, tiny
	hx, lx := hi(x), lo(x)
	hy, ly := hi(y), lo(y)
	ix := hx & 0x7fffffff
	iy := hy & 0x7fffffff

	if (uint32(iy) | ly) == 0 {
		return 1
	}
	if hx == 0x3ff00000 && lx == 0 {
		return 1
	}
	if ix > 0x7ff00000 || (ix == 0x7ff00000 && lx != 0) || iy > 0x7ff00000 || (iy == 0x7ff00000 && ly != 0) {
		return float64(x + y)
	}
	yisint := 0
	if hx < 0 {
		if iy >= 0x43400000 {
			yisint = 2
		} else if iy >= 0x3ff00000 {
			k := (iy >> 20) - 0x3ff
			if k > 20 {
				j := ly >> uint(52-k)
				if (j << uint(52-k)) == ly {
					yisint = 2 - int(j&1)
				}
			} else if ly == 0 {
				j := iy >> uint(20-k)
				if (j << uint(20-k)) == iy {
					yisint = 2 - int(j&1)
				}
			}
		}
	}
	if ly == 0 {
		if iy == 0x7ff00000 {
			switch {
			case ((ix - 0x3ff00000) | int32(lx)) == 0:
				return float64(y - y)
			case ix >= 0x3ff00000:
				if hy >= 0 {
					return y
				}
				return 0
			default:
				if hy < 0 {
					return -y
				}
				return 0
			}
		}
		if iy == 0x3ff00000 {
			if hy < 0 {
				return float64(1 / x)
			}
			return x
		}
		if hy == 0x40000000 {
			return float64(x * x)
		}
		if hy == 0x3fe00000 && hx >= 0 {
			return math.Sqrt(x)
		}
	}
	ax := math.Abs(x)
	if lx == 0 {
		if ix == 0x7ff00000 || ix == 0 || ix == 0x3ff00000 {
			z := ax
			if hy < 0 {
				z = float64(1 / z)
			}
			if hx < 0 {
				if ((ix - 0x3ff00000) | int32(yisint)) == 0 {
					z = float64((z - z) / (z - z))
				} else if yisint == 1 {
					z = -z
				}
			}
			return z
		}
	}
	n := int32((uint32(hx) >> 31) - 1)
	if (n | int32(yisint)) == 0 {
		return float64((x - x) / (x - x))
	}
	s := 1.0
	if (n | int32(yisint-1)) == 0 {
		s = -1
	}
	var t1, t2 float64
	if iy > 0x41e00000 {
		if iy > 0x43f00000 {
			if ix <= 0x3fefffff {
				if hy < 0 {
					return float64(hv * hv)
				}
				return float64(tv * tv)
			}
			if hy > 0 {
				return float64(hv * hv)
			}
			return float64(tv * tv)
		}
		if ix < 0x3fefffff {
			if hy < 0 {
				return float64(float64(s*hv) * huge)
			}
			return float64(float64(s*tv) * tiny)
		}
		if ix > 0x3ff00000 {
			if hy > 0 {
				return float64(float64(s*hv) * huge)
			}
			return float64(float64(s*tv) * tiny)
		}
		t := float64(ax - 1)
		w := float64(float64(t*t) * float64(0.5-float64(t*float64(0.3333333333333333333333-float64(t*0.25)))))
		u := float64(ivln2H * t)
		v := float64(float64(t*ivln2L) - float64(w*ivln2))
		t1 = zeroLo(float64(u + v))
		t2 = float64(v - float64(t1-u))
	} else {
		nn := int32(0)
		if ix < 0x00100000 {
			ax = float64(ax * two53)
			nn -= 53
			ix = hi(ax)
		}
		nn += ((ix) >> 20) - 0x3ff
		j := ix & 0x000fffff
		ix = j | 0x3ff00000
		k := 0
		switch {
		case j <= 0x3988E:
			k = 0
		case j < 0xBB67A:
			k = 1
		default:
			k = 0
			nn++
			ix -= 0x00100000
		}
		ax = withHi(ax, ix)
		bp := [2]float64{bp0, bp1}
		dpH := [2]float64{dp0, dp1}
		dpL := [2]float64{dl0, dl1}
		u := float64(ax - bp[k])
		v := float64(1 / float64(ax+bp[k]))
		ss := float64(u * v)
		sH := zeroLo(ss)
		tH := fromWords(((ix>>1)|0x20000000)+0x00080000+int32(k<<18), 0)
		tL := float64(ax - float64(tH-bp[k]))
		sL := float64(v * float64(float64(u-float64(sH*tH))-float64(sH*tL)))
		s2 := float64(ss * ss)
		rr := float64(float64(s2*s2) * float64(L1+float64(s2*float64(L2+float64(s2*float64(L3+float64(s2*float64(L4+float64(s2*float64(L5+float64(s2*L6)))))))))))
		rr = float64(rr + float64(sL*float64(sH+ss)))
		s2 = float64(sH * sH)
		tH = zeroLo(float64(float64(3.0+s2) + rr))
		tL = float64(rr - float64(float64(tH-3.0)-s2))
		u = float64(sH * tH)
		v = float64(float64(sL*tH) + float64(tL*ss))
		pH := zeroLo(float64(u + v))
		pL := float64(v - float64(pH-u))
		zH := float64(cpH * pH)
		zL := float64(float64(float64(cpL*pH)+float64(pL*cp)) + dpL[k])
		t := float64(nn)
		t1 = zeroLo(float64(float64(float64(zH+zL)+dpH[k]) + t))
		t2 = float64(zL - float64(float64(float64(t1-t)-dpH[k])-zH))
	}
	y1 := zeroLo(y)
	pL := float64(float64(float64(y-y1)*t1) + float64(y*t2))
	pH := float64(y1 * t1)
	z := float64(pL + pH)
	j := hi(z)
	i := int32(lo(z))
	if j >= 0x40900000 {
		if ((j - 0x40900000) | i) != 0 {
			return float64(float64(s*hv) * huge)
		}
		if float64(pL+ovt) > float64(z-pH) {
			return float64(float64(s*hv) * huge)
		}
	} else if (j & 0x7fffffff) >= 0x4090cc00 {
		if ((j - int32(-1064252416)) | i) != 0 {
			return float64(float64(s*tv) * tiny)
		}
		if pL <= float64(z-pH) {
			return float64(float64(s*tv) * tiny)
		}
	}
	i = j & 0x7fffffff
	k := (i >> 20) - 0x3ff
	nn := int32(0)
	if i > 0x3fe00000 {
		nn = j + (0x00100000 >> uint(k+1))
		k = ((nn & 0x7fffffff) >> 20) - 0x3ff
		t := fromWords(nn&^(0x000fffff>>uint(k)), 0)
		nn = ((nn & 0x000fffff) | 0x00100000) >> uint(20-k)
		if j < 0 {
			nn = -nn
		}
		pH = float64(pH - t)
	}
	t := zeroLo(float64(pL + pH))
	u := float64(t * lg2H)
	v := float64(float64(float64(pL-float64(t-pH))*lg2) + float64(t*lg2L))
	z = float64(u + v)
	w := float64(v - float64(z-u))
	t = float64(z * z)
	t1 = float64(z - float64(t*float64(P1+float64(t*float64(P2+float64(t*float64(P3+float64(t*float64(P4+float64(t*P5))))))))))
	rr := float64(float64(float64(z*t1)/float64(t1-2.0)) - float64(w+float64(z*w)))
	z = float64(1.0 - float64(rr-z))
	j = hi(z)
	j += nn << 20
	if (j >> 20) <= 0 {
		z = math.Ldexp(z, int(nn))
	} else {
		z = withHi(z, j)
	}
	return float64(s * z)
}
