import js from '@eslint/js';
import { defineConfig, globalIgnores } from 'eslint/config';
import tseslint from 'typescript-eslint';
import globals from 'globals';
import hooks from 'eslint-plugin-react-hooks';
import refresh from 'eslint-plugin-react-refresh';

export default defineConfig([
  globalIgnores(['**/dist/**', '**/node_modules/**', '.tmp/**']),
  {
    files: ['**/*.{js,mjs,ts,tsx}'],
    extends: [js.configs.recommended],
    languageOptions: { globals: globals.node },
  },
  {
    files: ['frontend/**/*.{ts,tsx}'],
    extends: [tseslint.configs.recommended],
    languageOptions: { globals: globals.browser },
  },
  {
    files: ['frontend/**/*.tsx'],
    extends: [hooks.configs.flat.recommended],
  },
  {
    files: ['frontend/apps/**/*.tsx'],
    extends: [refresh.configs.vite],
  },
]);
