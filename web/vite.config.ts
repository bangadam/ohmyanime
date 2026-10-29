import {defineConfig, loadEnv} from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig(({mode}) => {
  const target = loadEnv(mode, '.', '').MYANIME_API_TARGET || 'http://127.0.0.1:8767';
  return {plugins:[react()],server:{port:5173,strictPort:true,proxy:{'/api':target,'/media':target}}};
});
