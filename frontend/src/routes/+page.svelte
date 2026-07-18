<script lang="ts">
	import { TrendingUp, ShieldCheck, Layers, ArrowRight, Wallet, BarChart3, PieChart, Sparkles } from '@lucide/svelte';
    import { fade, slide } from 'svelte/transition';
</script>

<svelte:head>
	<title>BudgetScript — Projected Wealth &amp; Simulation Engine</title>
</svelte:head>

<div class="relative min-h-screen bg-slate-50 overflow-hidden">
	<!-- Background Ambient Glow -->
	<div class="absolute inset-0 z-0 pointer-events-none">
		<div class="absolute top-[-10%] left-[-10%] w-[50%] h-[50%] bg-indigo-200/20 blur-[120px] rounded-full"></div>
		<div class="absolute bottom-[-10%] right-[-10%] w-[50%] h-[50%] bg-purple-200/20 blur-[120px] rounded-full"></div>
	</div>

	<!-- Hero Section -->
	<header class="relative z-10 pt-32 pb-20 px-6 max-w-[1400px] mx-auto text-center" in:fade={{ duration: 800 }}>
		<div class="inline-flex items-center gap-2 px-4 py-2 bg-white rounded-full shadow-sm border border-slate-100 mb-8">
			<Sparkles class="w-4 h-4 text-indigo-600" />
			<span class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-500">Deterministic Wealth Engine</span>
		</div>

		<h1 class="text-7xl md:text-8xl font-black tracking-tighter text-slate-900 mb-8 leading-[0.9]">
			Project your <br />
			<span class="gradient-text">Financial Truth</span>.
		</h1>

		<p class="max-w-2xl mx-auto text-xl text-slate-500 font-medium leading-relaxed mb-12">
			A high-performance simulation engine for long-term wealth management.
			Multi-account synchronization, Monte Carlo risk modelling, and identity-bound encryption.
		</p>

		<div class="flex flex-col sm:flex-row items-center justify-center gap-6">
			<a href="/auth/login" class="btn-primary py-5 px-10 text-xl group shadow-2xl shadow-indigo-100">
				Access Engine
				<ArrowRight class="w-6 h-6 group-hover:translate-x-1 transition-transform" />
			</a>
			<a href="/auth/register" class="btn-secondary py-5 px-10 text-xl border-2">
				System Registration
			</a>
		</div>
	</header>

	<!-- Feature Grid -->
	<section class="relative z-10 py-32 px-6 max-w-[1400px] mx-auto">
		<div class="grid grid-cols-1 md:grid-cols-3 gap-8">
			<div class="glass-card p-10 space-y-6 group hover:border-indigo-600 transition-all duration-500" in:slide={{ delay: 200, duration: 600 }}>
				<div class="w-16 h-16 bg-indigo-50 rounded-3xl flex items-center justify-center text-indigo-600 group-hover:scale-110 transition-transform">
					<ShieldCheck class="w-8 h-8" />
				</div>
				<div class="space-y-2">
					<h3 class="text-2xl font-black text-slate-900 tracking-tight">Identity-Bound</h3>
					<p class="text-slate-500 text-lg leading-relaxed">
						Your data is encrypted using keys derived from your hardware security key. If the DB moves, it becomes unreadable.
					</p>
				</div>
			</div>

			<div class="glass-card p-10 space-y-6 group hover:border-emerald-600 transition-all duration-500" in:slide={{ delay: 400, duration: 600 }}>
				<div class="w-16 h-16 bg-emerald-50 rounded-3xl flex items-center justify-center text-emerald-600 group-hover:scale-110 transition-transform">
					<TrendingUp class="w-8 h-8" />
				</div>
				<div class="space-y-2">
					<h3 class="text-2xl font-black text-slate-900 tracking-tight">Monte Carlo Risk</h3>
					<p class="text-slate-500 text-lg leading-relaxed">
						Simulate thousands of market conditions using vectorized SIMD math to find your deterministic probability of success.
					</p>
				</div>
			</div>

			<div class="glass-card p-10 space-y-6 group hover:border-purple-600 transition-all duration-500" in:slide={{ delay: 600, duration: 600 }}>
				<div class="w-16 h-16 bg-purple-50 rounded-3xl flex items-center justify-center text-purple-600 group-hover:scale-110 transition-transform">
					<Layers class="w-8 h-8" />
				</div>
				<div class="space-y-2">
					<h3 class="text-2xl font-black text-slate-900 tracking-tight">Plan Forking</h3>
					<p class="text-slate-500 text-lg leading-relaxed">
						Immutable snapshots of financial plans. Compare deltas between different asset allocations or payment strategies.
					</p>
				</div>
			</div>
		</div>
	</section>
</div>
