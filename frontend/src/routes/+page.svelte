<script>
  const statusItems = [
    { label: 'Backend', value: 'Bootstrap server' },
    { label: 'Database', value: 'PostgreSQL planned' },
    { label: 'Provider', value: 'Codex/OpenAI OAuth planned' }
  ];
</script>

<svelte:head>
  <title>N2API Admin</title>
</svelte:head>

<main class="min-h-screen bg-[#f6f7f9] text-slate-950">
  <section class="mx-auto flex w-full max-w-6xl flex-col gap-8 px-6 py-8">
    <header class="flex flex-wrap items-center justify-between gap-4 border-b border-slate-200 pb-6">
      <div>
        <p class="text-sm font-medium text-slate-500">Personal AI Gateway</p>
        <h1 class="mt-1 text-3xl font-semibold tracking-normal text-slate-950">N2API</h1>
      </div>
      <div class="rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm font-medium text-emerald-800">
        Bootstrap
      </div>
    </header>

    <div class="grid gap-4 md:grid-cols-3">
      {#each statusItems as item}
        <article class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
          <p class="text-sm font-medium text-slate-500">{item.label}</p>
          <p class="mt-2 text-lg font-semibold text-slate-950">{item.value}</p>
        </article>
      {/each}
    </div>

    <section class="rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
      <h2 class="text-lg font-semibold text-slate-950">V1 Scope</h2>
      <div class="mt-4 grid gap-3 text-sm text-slate-700 md:grid-cols-2">
        <div class="rounded-md bg-slate-50 p-4">OpenAI-compatible API routes</div>
        <div class="rounded-md bg-slate-50 p-4">Codex-oriented adapter behavior</div>
        <div class="rounded-md bg-slate-50 p-4">Single admin password</div>
        <div class="rounded-md bg-slate-50 p-4">PostgreSQL-backed configuration</div>
      </div>
    </section>
  </section>
</main>
