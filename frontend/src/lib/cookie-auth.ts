import { createCipheriv, createDecipheriv, randomBytes } from 'crypto';
import { cookies } from 'next/headers';

// Debug logging utility
const DEBUG_ENABLED = process.env.NODE_ENV !== 'production' && process.env.DEBUG_LOGS !== 'false';
const debugLog = (...args: unknown[]) => {
  if (DEBUG_ENABLED) {
    console.log(...args);
  }
};

const COOKIE_NAME = 'agentapi_token';
const ALGORITHM = 'aes-256-gcm';
const IV_LENGTH = 16;
const TAG_LENGTH = 16;

export const AUTH_COOKIE_NAME = COOKIE_NAME;
export const AUTH_COOKIE_VERSION = 1 as const;

export type AuthCookiePayload =
  | {
      version: typeof AUTH_COOKIE_VERSION;
      type: 'api_key';
      access_token: string;
    }
  | {
      version: typeof AUTH_COOKIE_VERSION;
      type: 'github_oauth';
      session_id: string;
      access_token: string;
    };

export const AUTH_COOKIE_OPTIONS = {
  httpOnly: true,
  secure: true,
  sameSite: 'strict' as const,
  maxAge: 30 * 24 * 60 * 60,
  path: '/',
};

function getEncryptionKey(): Buffer {
  const secret = process.env.COOKIE_ENCRYPTION_SECRET;
  if (!secret) {
    console.error('COOKIE_ENCRYPTION_SECRET environment variable is not set. Cookie authentication will not work.');
    throw new Error('COOKIE_ENCRYPTION_SECRET environment variable is required');
  }
  if (!/^[0-9a-fA-F]{64}$/.test(secret)) {
    console.error(`COOKIE_ENCRYPTION_SECRET length is ${secret.length}, but must be exactly 64 hex characters (32 bytes).`);
    throw new Error('COOKIE_ENCRYPTION_SECRET must be exactly 32 bytes (64 hex characters)');
  }
  return Buffer.from(secret, 'hex');
}

function encryptValue(value: string): string {
  const key = getEncryptionKey();
  const iv = randomBytes(IV_LENGTH);
  
  const cipher = createCipheriv(ALGORITHM, key, iv);
  
  let encrypted = cipher.update(value, 'utf8', 'hex');
  encrypted += cipher.final('hex');
  
  const tag = cipher.getAuthTag();
  
  // Combine iv + tag + encrypted data
  const combined = Buffer.concat([iv, tag, Buffer.from(encrypted, 'hex')]);
  return combined.toString('base64');
}

function decryptValue(encryptedData: string): string {
  const key = getEncryptionKey();
  const combined = Buffer.from(encryptedData, 'base64');
  
  if (combined.length < IV_LENGTH + TAG_LENGTH) {
    throw new Error('Invalid encrypted cookie')
  }

  // Extract components
  const iv = combined.subarray(0, IV_LENGTH);
  const tag = combined.subarray(IV_LENGTH, IV_LENGTH + TAG_LENGTH);
  const encrypted = combined.subarray(IV_LENGTH + TAG_LENGTH);
  
  const decipher = createDecipheriv(ALGORITHM, key, iv);
  decipher.setAuthTag(tag);
  
  let decrypted = decipher.update(encrypted, undefined, 'utf8');
  decrypted += decipher.final('utf8');
  
  return decrypted;
}

function isAuthCookiePayload(value: unknown): value is AuthCookiePayload {
  if (!value || typeof value !== 'object') return false;

  const payload = value as Partial<AuthCookiePayload>;
  if (
    payload.version !== AUTH_COOKIE_VERSION
    || (payload.type !== 'api_key' && payload.type !== 'github_oauth')
    || typeof payload.access_token !== 'string'
    || payload.access_token.length === 0
  ) {
    return false;
  }

  return payload.type !== 'github_oauth'
    || (typeof payload.session_id === 'string' && payload.session_id.length > 0);
}

export function encryptAuthCookie(payload: AuthCookiePayload): string {
  return encryptValue(JSON.stringify(payload));
}

export function encryptApiKey(apiKey: string): string {
  return encryptAuthCookie({
    version: AUTH_COOKIE_VERSION,
    type: 'api_key',
    access_token: apiKey,
  });
}

export function encryptOAuthSession(sessionId: string, accessToken: string): string {
  return encryptAuthCookie({
    version: AUTH_COOKIE_VERSION,
    type: 'github_oauth',
    session_id: sessionId,
    access_token: accessToken,
  });
}

export function decryptAuthCookie(encryptedData: string): AuthCookiePayload {
  const decrypted = decryptValue(encryptedData);

  let payload: unknown;
  try {
    payload = JSON.parse(decrypted);
  } catch {
    // Legacy cookies encrypted the raw API key instead of a JSON payload.
    if (!decrypted) {
      throw new Error('Invalid authentication cookie payload');
    }
    return {
      version: AUTH_COOKIE_VERSION,
      type: 'api_key',
      access_token: decrypted,
    };
  }

  if (!isAuthCookiePayload(payload)) {
    throw new Error('Invalid authentication cookie payload');
  }
  return payload;
}

export function decryptApiKey(encryptedData: string): string {
  return decryptAuthCookie(encryptedData).access_token;
}

export async function setApiKeyCookie(apiKey: string): Promise<void> {
  const encryptedApiKey = encryptApiKey(apiKey);
  const cookieStore = await cookies();
  
  cookieStore.set(COOKIE_NAME, encryptedApiKey, AUTH_COOKIE_OPTIONS);
}

export async function getAuthSessionFromCookie(): Promise<AuthCookiePayload | null> {
  try {
    const cookieStore = await cookies();
    const encryptedCookie = cookieStore.get(COOKIE_NAME)?.value;

    if (!encryptedCookie) {
      debugLog('No agentapi_token cookie found');
      return null;
    }

    return decryptAuthCookie(encryptedCookie);
  } catch (error) {
    console.error('Failed to decrypt authentication cookie:', error);
    return null;
  }
}

export async function getApiKeyFromCookie(): Promise<string | null> {
  const authSession = await getAuthSessionFromCookie();
  return authSession?.access_token ?? null;
}

export async function renewApiKeyCookie(): Promise<void> {
  try {
    const cookieStore = await cookies();
    const encryptedApiKey = cookieStore.get(COOKIE_NAME)?.value;
    
    if (!encryptedApiKey) {
      debugLog('No agentapi_token cookie found to renew');
      return;
    }
    
    // Re-set the cookie to renew its expiration
    cookieStore.set(COOKIE_NAME, encryptedApiKey, AUTH_COOKIE_OPTIONS);
    
    debugLog('Renewed agentapi_token cookie expiration');
  } catch (error) {
    console.error('Failed to renew API key cookie:', error);
  }
}

export async function deleteApiKeyCookie(): Promise<void> {
  const cookieStore = await cookies();
  // Set the cookie with maxAge=0 to ensure it's deleted
  cookieStore.set(COOKIE_NAME, '', { ...AUTH_COOKIE_OPTIONS, maxAge: 0 });
}
