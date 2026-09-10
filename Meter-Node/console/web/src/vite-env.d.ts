/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Set for the standalone preview build; see main.tsx. */
  readonly VITE_HASH_ROUTER?: string;
}
interface ImportMeta {
  readonly env: ImportMetaEnv;
}
