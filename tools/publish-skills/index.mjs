import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'

const root = process.cwd()
const source = path.join(root, 'skills')
const dist = path.join(root, 'dist-skills')
const idPattern = /^[a-z0-9][a-z0-9-]{0,63}$/
const versionPattern = /^v?\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?$/
const maxBody = 256 * 1024

function fail(message) {
  console.error('publish-skills:', message)
  process.exit(1)
}

function splitFrontmatter(raw) {
  const text = raw.replace(/^\uFEFF/, '')
  if (!text.startsWith('---\n')) return null
  const rest = text.slice(4)
  const end = rest.indexOf('\n---')
  if (end < 0) return null
  return { front: rest.slice(0, end).trim(), body: rest.slice(end + 4).trim() }
}

fs.rmSync(dist, { recursive: true, force: true })
fs.mkdirSync(path.join(dist, 'bundles'), { recursive: true })

const seen = new Set()
const skills = []
for (const entry of fs.readdirSync(source, { withFileTypes: true })) {
  if (!entry.isDirectory() || entry.name === 'third_party') continue
  const raw = fs.readFileSync(path.join(source, entry.name, 'SKILL.md'), 'utf8')
  const parts = splitFrontmatter(raw)
  const metaPath = path.join(source, entry.name, 'skill.json')
  let meta
  try {
    meta = JSON.parse(fs.existsSync(metaPath) ? fs.readFileSync(metaPath, 'utf8') : (parts && parts.front))
  } catch (e) { fail(`${entry.name}: invalid metadata: ${e.message}`) }
  if (!meta || typeof meta !== 'object') fail(`${entry.name}: metadata is not an object`)
  const body = parts ? parts.body : raw.trim()
  if (meta.id !== entry.name || !idPattern.test(meta.id) || seen.has(meta.id)) fail(`${entry.name}: invalid or duplicate id`)
  seen.add(meta.id)
  if (!meta.name || !meta.description || meta.description.length > 1200) fail(`${entry.name}: invalid name or description`)
  if (!versionPattern.test(meta.version || '')) fail(`${entry.name}: invalid version`)
  if (meta.channel && meta.channel !== 'stable' && meta.channel !== 'beta') fail(`${entry.name}: invalid channel`)
  if (!Array.isArray(meta.connectors) || !Array.isArray(meta.allowed_tools)) fail(`${entry.name}: connectors and allowed_tools must be arrays`)
  if (!body || Buffer.byteLength(body) > maxBody) fail(`${entry.name}: body is empty or too large`)

  const bundle = Buffer.from(JSON.stringify({ id: meta.id, version: meta.version, body }))
  const sha256 = crypto.createHash('sha256').update(bundle).digest('hex')
  fs.writeFileSync(path.join(dist, 'bundles', `${meta.id}@${meta.version}.json`), bundle)
  skills.push({ ...meta, sha256, size: bundle.length })
}

skills.sort((a, b) => a.id.localeCompare(b.id))
const index = Buffer.from(JSON.stringify({
  schema: 'argus-skills/v1',
  generated_at: new Date().toISOString(),
  skills,
}, null, 2))
fs.writeFileSync(path.join(dist, 'index.json'), index)

const key = process.env.SKILLS_SIGNING_KEY
if (!key) fail('SKILLS_SIGNING_KEY is not set')
const signature = crypto.sign(null, index, crypto.createPrivateKey(key))
fs.writeFileSync(path.join(dist, 'index.json.sig'), signature)

console.log(`built ${skills.length} skills, index ${index.length} bytes`)
