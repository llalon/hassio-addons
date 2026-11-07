import { readFile, readdir, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

async function replaceExactlyOnce(path, before, after) {
  const source = await readFile(path, 'utf8')
  const matches = source.split(before).length - 1
  if (matches !== 1) {
    throw new Error(`Expected one match in ${path}, found ${matches}`)
  }
  await writeFile(path, source.replace(before, after))
}

await replaceExactlyOnce(
  'vite.config.ts',
  'export default defineConfig({',
  "export default defineConfig({\n    base: './',"
)

await replaceExactlyOnce(
  'src/router.tsx',
  '\n])\n',
  "\n], { basename: window.__RCLONE_INGRESS_BASE__ ?? '/' })\n"
)

await replaceExactlyOnce(
  'src/lib/store.ts',
  "            url: '',",
  "            url: window.__RCLONE_INGRESS_URL__ ?? '',"
)

await replaceExactlyOnce(
  'src/lib/store.ts',
  "            storage: createJSONStorage<PersistedStore>(() => localStorage),",
  `            storage: createJSONStorage<PersistedStore>(() => localStorage),
            merge: (persistedState, currentState) => ({
                ...currentState,
                ...(persistedState as Partial<PersistedStore>),
                url: window.__RCLONE_INGRESS_URL__ ?? '',
                user: '',
                pass: '',
            }),`
)

await replaceExactlyOnce(
  'src/rclone/client.ts',
  "    if (window.location.pathname === '/login') {",
  "    if (window.location.pathname.endsWith('/login')) {"
)

await replaceExactlyOnce(
  'src/rclone/client.ts',
  "    window.location.replace('/login?reason=auth')",
  '    window.location.replace(`${(window.__RCLONE_INGRESS_BASE__ ?? "").replace(/\\/$/, "")}/login?reason=auth`)'
)

await replaceExactlyOnce(
  'src/rclone/client.ts',
  "    const url = new URL(auth.url.replace(/^\\//, ''), '/operations/uploadfile')",
  "    const url = new URL(`${auth.url.replace(/\\/+$/, '')}/operations/uploadfile`)"
)

await replaceExactlyOnce(
  'index.html',
  'href="/icon.svg"',
  'href="icon.svg"'
)

async function makeIconPathsRelative(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      await makeIconPathsRelative(path)
      continue
    }
    if (!entry.name.endsWith('.tsx') && !entry.name.endsWith('.ts')) continue

    const source = await readFile(path, 'utf8')
    const updated = source.replaceAll('"/icon.svg"', '"icon.svg"')
    if (source !== updated) await writeFile(path, updated)
  }
}

await makeIconPathsRelative('src')

await writeFile(
  'src/ingress.d.ts',
  `export {}\ndeclare global {\n  interface Window {\n    __RCLONE_INGRESS_BASE__?: string\n    __RCLONE_INGRESS_URL__?: string\n  }\n}\n`
)
