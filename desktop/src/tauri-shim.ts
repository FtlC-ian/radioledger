/**
 * Browser shim for Tauri APIs
 * Provides fallback implementations when running in web context
 */

const API_BASE = window.location.origin.includes('6173') 
  ? 'http://localhost:9080' 
  : '/api';

let authToken: string | null = localStorage.getItem('auth_token');

export function isTauri(): boolean {
  return !!(window as any).__TAURI__;
}

export async function invoke(cmd: string, args?: any): Promise<any> {
  // If Tauri is available, use it
  if (isTauri()) {
    const { invoke: tauriInvoke } = await import('@tauri-apps/api/core');
    return tauriInvoke(cmd, args);
  }

  // Browser fallback
  switch (cmd) {
    case 'get_auth_status':
      if (!authToken) {
        return { logged_in: false, callsign: null };
      }
      try {
        const res = await fetch(`${API_BASE}/v1/auth/me`, {
          headers: { Authorization: `Bearer ${authToken}` }
        });
        if (!res.ok) {
          authToken = null;
          localStorage.removeItem('auth_token');
          return { logged_in: false, callsign: null };
        }
        const data = await res.json();
        return {
          logged_in: true,
          callsign: data.data.callsign || null
        };
      } catch {
        return { logged_in: false, callsign: null };
      }

    case 'login':
      const email = prompt('Enter your email:');
      if (!email) throw new Error('Login cancelled');
      
      const loginRes = await fetch(`${API_BASE}/v1/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email })
      });
      
      if (!loginRes.ok) {
        const registerRes = await fetch(`${API_BASE}/v1/auth/register`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email, callsign: 'W1AW' })
        });
        const regData = await registerRes.json();
        if (!regData.success) throw new Error(regData.error || 'Registration failed');
        authToken = regData.data.token;
      } else {
        const loginData = await loginRes.json();
        if (!loginData.success) throw new Error(loginData.error || 'Login failed');
        authToken = loginData.data.token;
      }
      
      localStorage.setItem('auth_token', authToken!);
      return { logged_in: true, callsign: null };

    case 'logout':
      authToken = null;
      localStorage.removeItem('auth_token');
      return { logged_in: false, callsign: null };

    case 'get_udp_status':
      return { listening: false, port: 2237, packets_received: 0 };

    case 'get_udp_config':
      return {
        wsjtx_port: 2237,
        wsjtx_auto_start: false,
        wsjtx_multicast_group: null,
        js8call_port: 2242,
        js8call_auto_start: false,
        n1mm_port: 12060,
        n1mm_auto_start: false,
        ft8battle_relay_enabled: false,
      };

    case 'start_udp_listener':
    case 'stop_udp_listener':
      return { listening: false, port: 2237, packets_received: 0 };

    case 'save_udp_settings':
      return null;

    case 'get_rig_status':
      return {
        connected: false,
        backend: null,
        host: null,
        port: null,
        frequency_hz: null,
        frequency_display: null,
        mode: null,
        band: null,
        bandwidth_hz: null,
        s_meter: null,
        power: null,
        vfo: null,
        strength: null,
        last_error: null
      };

    case 'refresh_rig':
      return null;

    case 'get_sync_status':
      return { pending: 0, last_sync: null, last_error: null };

    case 'sync_now':
      return { pending: 0, last_sync: new Date().toISOString(), last_error: null };

    default:
      console.warn(`Unhandled Tauri command in browser shim: ${cmd}`);
      return null;
  }
}

export async function listen(event: string, handler: (e: any) => void): Promise<() => void> {
  if (isTauri()) {
    const { listen: tauriListen } = await import('@tauri-apps/api/event');
    return tauriListen(event, handler);
  }
  // Browser: no-op, return empty unsubscribe
  return () => {};
}
