'use client';

import { useState, useEffect, useRef, useCallback } from 'react';

/**
 * Page Visibility API を使用してブラウザがバックグラウンドかどうかを追跡するカスタムフック
 * @returns {boolean} ページが表示されている場合は true、バックグラウンドの場合は false
 */
export function usePageVisibility(): boolean {
  const [isVisible, setIsVisible] = useState<boolean>(true);

  useEffect(() => {
    // 初期状態を設定
    setIsVisible(!document.hidden);

    const handleVisibilityChange = () => {
      setIsVisible(!document.hidden);
    };

    // visibilitychange イベントリスナーを追加
    document.addEventListener('visibilitychange', handleVisibilityChange);

    // クリーンアップ
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, []);

  return isVisible;
}

/**
 * バックグラウンド制御機能付きの setInterval フック
 * ページがバックグラウンドになったときに自動的にインターバルを停止し、
 * フォアグラウンドに戻ったときに再開する
 * 
 * @param callback 実行する関数
 * @param delay インターバルの間隔（ミリ秒）
 * @param immediate 最初に即座に実行するかどうか（デフォルト: true）
 * @returns インターバルの状態とコントロール機能
 */
export function useBackgroundAwareInterval(
  callback: () => void,
  delay: number,
  immediate: boolean = true
): {
  isRunning: boolean;
  start: () => void;
  stop: () => void;
  restart: () => void;
} {
  const intervalIdRef = useRef<NodeJS.Timeout | null>(null);
  const [isRunning, setIsRunning] = useState(false);
  const isVisible = usePageVisibility();
  
  // useRefでcallbackの最新版を保持し、再レンダリングを防ぐ
  const callbackRef = useRef(callback);
  callbackRef.current = callback;

  // 現在のdelayを保持するref
  const delayRef = useRef(delay);

  const start = useCallback(() => {
    if (intervalIdRef.current) return; // 既に動作中の場合は何もしない

    if (immediate) {
      callbackRef.current();
    }

    const id = setInterval(() => callbackRef.current(), delayRef.current);
    intervalIdRef.current = id;
    setIsRunning(true);
  }, [immediate]);

  const stop = useCallback(() => {
    if (intervalIdRef.current) {
      clearInterval(intervalIdRef.current);
      intervalIdRef.current = null;
      setIsRunning(false);
    }
  }, []);

  const restart = useCallback(() => {
    stop();
    start();
  }, [stop, start]);

  // ページの表示状態が変化したときの処理
  useEffect(() => {
    if (isVisible && !intervalIdRef.current && isRunning) {
      // ページがフォアグラウンドに戻り、インターバルが停止していて実行中の状態の場合は再開
      start();
    } else if (!isVisible && intervalIdRef.current) {
      // ページがバックグラウンドになった場合はインターバルを停止
      clearInterval(intervalIdRef.current);
      intervalIdRef.current = null;
    }
  }, [isVisible, isRunning, start]);

  // delayが変更された場合にインターバルを再起動
  useEffect(() => {
    if (delayRef.current !== delay && intervalIdRef.current) {
      delayRef.current = delay;
      // 実行中の場合のみ再起動
      if (isRunning) {
        clearInterval(intervalIdRef.current);
        intervalIdRef.current = null;
        const id = setInterval(() => callbackRef.current(), delay);
        intervalIdRef.current = id;
      }
    } else {
      delayRef.current = delay;
    }
  }, [delay, isRunning]);

  // コンポーネントのアンマウント時にクリーンアップ
  useEffect(() => {
    return () => {
      if (intervalIdRef.current) {
        clearInterval(intervalIdRef.current);
        intervalIdRef.current = null;
      }
    };
  }, []);

  return { isRunning, start, stop, restart };
}