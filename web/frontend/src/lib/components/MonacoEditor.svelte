<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    

    import { onMount, onDestroy } from 'svelte';

    let { value = $bindable(''), readOnly = false } = $props();

    let container: HTMLDivElement | null = $state(null);
    let editor: any = null;
    let loading = $state(true);

    const typings = `
declare module 'wealthengine' {
	export class Income {
		plannedValue(): number;
	}

	export class BudgetSheet {
		income(name: string): Income;
	}

	export class RealtimeAccount {
		balance(): number;
		sync(): Promise<void>;
	}

	export class Realtime {
		account(key: string): RealtimeAccount;
	}

	export class WealthEngine {
		currentBudgetSheet(): BudgetSheet;
		realtime(): Realtime;
	}
}

// Injected globals
declare const secrets: Record<string, string>;
declare const trigger: {
	type: 'CRON' | 'TRANSACTION' | 'SYNC_FINISHED';
	data: {
		id?: string;
		amount?: number;
		receiver?: string;
		description?: string;
		integration_id: string;
		account_id?: string;
		timestamp: string;
		integration_name?: string;
		service_type?: string;
		discovered_transactions?: number;
	};
};

// Fallback wildcard to allow arbitrary external dependency imports
declare module '*';
`;

    let dependencyDisposables: any[] = [];
    let currentDepsString = '';
    let debounceTimeout: any = null;
    let fetchingDeps = $state(false);

    function parseDepsFromCode(code: string) {
        const depRegex = /\/\*\*\s*depend\s+on\s+([a-zA-Z0-9_\-]+):([0-9\.]+)\s*\*\*\//g;
        const deps = [];
        let match;
        while ((match = depRegex.exec(code)) !== null) {
            const pkg = match[1];
            const ver = match[2];
            if (pkg !== 'wealthengine') {
                deps.push({ pkg, ver });
            }
        }
        return deps;
    }

    function parseRelativePaths(content: string) {
        const paths: string[] = [];
        const importExportRegex = /(?:import|export)\s+[\s\S]*?\s+from\s+['"](\.\.?\/[^'"]+)['"]/g;
        const referenceRegex = /<reference\s+path\s*=\s*['"](\.\.?\/[^'"]+)['"]/g;
        const dynamicImportRegex = /import\(['"](\.\.?\/[^'"]+)['"]\)/g;

        let match;
        while ((match = importExportRegex.exec(content)) !== null) {
            paths.push(match[1]);
        }
        while ((match = referenceRegex.exec(content)) !== null) {
            paths.push(match[1]);
        }
        while ((match = dynamicImportRegex.exec(content)) !== null) {
            paths.push(match[1]);
        }
        return [...new Set(paths)];
    }

    function resolveRelativeUrl(baseUrl: string, relativePath: string) {
        let resolved = relativePath;
        if (!resolved.endsWith('.d.ts') && !resolved.endsWith('.ts') && !resolved.endsWith('.json')) {
            resolved = resolved.replace(/\/$/, '');
            resolved = resolved + '.d.ts';
        }
        return new URL(resolved, baseUrl).toString();
    }

    async function loadPackageTypings(pkgName: string, version: string) {
        const monaco = (window as any).monaco;
        if (!monaco) return;

        const entryUrl = `https://esm.sh/${pkgName}@${version}`;
        const loadedUrls = new Set<string>();

        try {
            const response = await fetch(entryUrl);
            const typesUrl = response.headers.get('x-typescript-types');
            if (!typesUrl) {
                console.warn(`[Monaco Typings] No types header found for ${pkgName}@${version}`);
                return;
            }

            const packageRootUrl = `https://esm.sh/${pkgName}@${version}/`;

            async function fetchDts(url: string) {
                if (loadedUrls.has(url)) return;
                loadedUrls.add(url);

                if (loadedUrls.size > 80) {
                    console.warn(`[Monaco Typings] Max d.ts files limit reached for ${pkgName}`);
                    return;
                }

                try {
                    const res = await fetch(url);
                    if (!res.ok) return;
                    const content = await res.text();

                    // Find where this file sits relative to package root
                    let relativePath = '';
                    if (url.startsWith(packageRootUrl)) {
                        relativePath = url.substring(packageRootUrl.length);
                    } else {
                        const urlObj = new URL(url);
                        relativePath = urlObj.pathname.replace(/^\/[^/]+\//, '');
                    }

                    const monacoPath = `file:///node_modules/${pkgName}/${relativePath}`;
                    const disp = monaco.languages.typescript.typescriptDefaults.addExtraLib(content, monacoPath);
                    dependencyDisposables.push(disp);

                    // Crawl relative imports
                    const imports = parseRelativePaths(content);
                    const crawlPromises = imports.map(rel => {
                        const nextUrl = resolveRelativeUrl(url, rel);
                        return fetchDts(nextUrl);
                    });
                    await Promise.all(crawlPromises);
                } catch (e) {
                    console.error(`[Monaco Typings] Failed to load d.ts file ${url}:`, e);
                }
            }

            await fetchDts(typesUrl);

            // Register redirect if entrypoint is not exactly index.d.ts at the package root
            const rootEntryPath = typesUrl.substring(packageRootUrl.length);
            if (rootEntryPath !== 'index.d.ts' && rootEntryPath !== 'index.ts') {
                const entryNoExt = rootEntryPath.replace(/\.d\.ts$/, '').replace(/\.ts$/, '');
                const redirectContent = `
export * from './${entryNoExt}';
import _default from './${entryNoExt}';
export default _default;
`;
                const redirectPath = `file:///node_modules/${pkgName}/index.d.ts`;
                const disp = monaco.languages.typescript.typescriptDefaults.addExtraLib(redirectContent, redirectPath);
                dependencyDisposables.push(disp);
            }
        } catch (err) {
            console.error(`[Monaco Typings] Failed to fetch types for ${pkgName}@${version}:`, err);
        }
    }

    async function syncDependencies(code: string) {
        const monaco = (window as any).monaco;
        if (!monaco) return;

        const deps = parseDepsFromCode(code);
        const depsString = deps;
        if (depsString === currentDepsString) return;
        currentDepsString = depsString;

        fetchingDeps = true;

        // Clear existing disposables
        for (const disp of dependencyDisposables) {
            disp.dispose();
        }
        dependencyDisposables = [];

        if (deps.length === 0) {
            fetchingDeps = false;
            return;
        }

        try {
            await Promise.all(deps.map(dep => loadPackageTypings(dep.pkg, dep.ver)));
        } catch (e) {
            console.error('[Monaco Typings] Dependency sync failed:', e);
        } finally {
            fetchingDeps = false;
        }
    }

    function loadMonaco() {
        if ((window as any).monaco) {
            initEditor();
            return;
        }

        const loaderScript = document.createElement('script');
        loaderScript.src = 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.45.0/min/vs/loader.min.js';
        loaderScript.onload = () => {
            const amdRequire = (window as any).require;
            amdRequire.config({
                paths: { vs: 'https://cdnjs.cloudflare.com/ajax/libs/monaco-editor/0.45.0/min/vs' }
            });

            amdRequire(['vs/editor/editor.main'], () => {
                initEditor();
            });
        };
        document.head.appendChild(loaderScript);
    }

    function initEditor() {
        if (!container) return;

        const monaco = (window as any).monaco;

        // Register custom typings
        monaco.languages.typescript.typescriptDefaults.setCompilerOptions({
            target: monaco.languages.typescript.ScriptTarget.ES2020,
            allowNonTsExtensions: true,
            moduleResolution: monaco.languages.typescript.ModuleResolutionKind.NodeJs,
        });

        // Clear existing extra libs if any to avoid duplication
        monaco.languages.typescript.typescriptDefaults.setExtraLibs([]);
        
        // Define soft-warm Monokai theme
        monaco.editor.defineTheme('monokai-soft', {
            base: 'vs-dark',
            inherit: true,
            rules: [
                { token: '', foreground: 'F8F8F2', background: '272822' },
                { token: 'comment', foreground: '75715E', fontStyle: 'italic' },
                { token: 'keyword', foreground: 'F92672', fontStyle: 'bold' },
                { token: 'number', foreground: 'AE81FF' },
                { token: 'string', foreground: 'E6DB74' },
                { token: 'regexp', foreground: 'E6DB74' },
                { token: 'type', foreground: '66D9EF', fontStyle: 'italic' },
                { token: 'class', foreground: 'A6E22E' },
                { token: 'function', foreground: 'A6E22E' },
                { token: 'variable', foreground: 'F8F8F2' },
                { token: 'variable.predefined', foreground: '66D9EF' },
                { token: 'keyword.operator', foreground: 'F92672' },
                { token: 'tag', foreground: 'F92672' },
                { token: 'tag.id', foreground: 'FD971F' },
                { token: 'tag.class', foreground: 'A6E22E' },
                { token: 'meta.selector', foreground: 'F92672' },
                { token: 'attribute.name', foreground: 'A6E22E' },
                { token: 'attribute.value', foreground: 'E6DB74' }
            ],
            colors: {
                'editor.background': '#272822',
                'editor.foreground': '#F8F8F2',
                'editor.lineHighlightBackground': '#3E3D32',
                'editorCursor.foreground': '#F8F8F0',
                'editor.selectionBackground': '#49483E',
                'editor.inactiveSelectionBackground': '#49483E',
                'editorLineNumber.foreground': '#90908A',
                'editorLineNumber.activeForeground': '#C2C2BF',
                'editorWidget.background': '#272822',
                'editorWidget.border': '#3E3D32'
            }
        });

        monaco.languages.typescript.typescriptDefaults.addExtraLib(
            typings,
            'file:///globals.d.ts'
        );

        // Associate with an explicit model URI so Monaco autocompletes global triggers correctly
        const modelUri = monaco.Uri.parse('file:///main.ts');
        let model = monaco.editor.getModel(modelUri);
        if (model) {
            model.setValue(value || '');
        } else {
            model = monaco.editor.createModel(value || '', 'typescript', modelUri);
        }

        editor = monaco.editor.create(container, {
            model: model,
            theme: 'monokai-soft',
            automaticLayout: true,
            readOnly: readOnly,
            minimap: { enabled: false },
            fontSize: 14,
            fontFamily: "'Fira Code', 'Courier New', Courier, monospace",
            lineHeight: 22,
            padding: { top: 12, bottom: 12 },
            roundedSelection: true,
            scrollBeyondLastLine: false,
            cursorBlinking: 'smooth',
            cursorSmoothCaretAnimation: 'on',
            scrollbar: {
                verticalScrollbarSize: 10,
                horizontalScrollbarSize: 10,
                useShadows: false
            }
        });

        editor.onDidChangeModelContent(() => {
            const currentVal = editor.getValue();
            if (value !== currentVal) {
                value = currentVal;
            }
        });

        // Trigger initial sync of dependencies
        syncDependencies(value || '');

        loading = false;
    }

    // React to value updates from parent
    $effect(() => {
        const val = value; // Unconditional read to register Svelte 5 dynamic dependency
        if (editor && val !== editor.getValue()) {
            editor.setValue(val || '');
        }
    });

    // React to readOnly updates
    $effect(() => {
        const ro = readOnly; // Unconditional read to register Svelte 5 dynamic dependency
        if (editor) {
            editor.updateOptions({ readOnly: ro });
        }
    });

    // Debounced watcher for dependency imports
    $effect(() => {
        const code = value || '';
        if (debounceTimeout) clearTimeout(debounceTimeout);
        debounceTimeout = setTimeout(() => {
            syncDependencies(code);
        }, 1000);
    });

    onMount(() => {
        loadMonaco();
    });

    onDestroy(() => {
        if (editor) {
            editor.dispose();
        }
        // Retain the shared virtual model so switching inline/fullscreen editors works seamlessly
        for (const disp of dependencyDisposables) {
            disp.dispose();
        }
        if (debounceTimeout) clearTimeout(debounceTimeout);
    });
</script>

<div class="relative w-full h-full min-h-[450px] rounded-xl overflow-hidden border border-slate-200 shadow-inner" style="background-color: #272822;">
    {#if loading}
        <div class="absolute inset-0 flex flex-col items-center justify-center text-slate-400 space-y-4" style="background-color: #272822;">
            <svg class="animate-spin h-8 w-8 text-indigo-500" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
            <p class="text-xs font-black uppercase tracking-[0.2em] animate-pulse text-indigo-400">Spawning Sandbox Editor...</p>
        </div>
    {/if}
    
    {#if fetchingDeps}
        <div class="absolute top-3 right-3 z-10 flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-indigo-500/10 border border-indigo-500/20 text-indigo-400 text-[10px] font-medium backdrop-blur-md animate-pulse">
            <svg class="animate-spin h-3 w-3 text-indigo-400" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
            <span>Syncing typings...</span>
        </div>
    {/if}

    <div bind:this={container} class="w-full h-full min-h-[450px]"></div>
</div>
