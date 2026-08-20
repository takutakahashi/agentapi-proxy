# プロファイル機能設計書

## 設計概要

agentapi-ui にプロファイル機能を追加し、ユーザーが複数の接続設定とリポジトリ履歴を管理できるようにする。新規セッション作成時にプロファイルを選択することで、異なる環境やプロジェクトでの作業を効率化する。

## 機能要件

### 1. プロファイル情報の管理
- agentapi proxy の接続先エンドポイントとAPIキー（トークン）
- リポジトリ履歴（最近使用したリポジトリの記録）
- プロファイル名とアイコン（オプション）
- プロファイルの作成日時・更新日時

### 2. プロファイル選択機能
- 新規セッション作成時のプロファイル選択UI
- デフォルトプロファイルの設定
- プロファイル一覧表示

### 3. プロファイル管理機能
- プロファイルの作成・編集・削除
- プロファイル設定のインポート・エクスポート
- プロファイル間での設定のコピー

## データ構造設計

### Profile インターface

```typescript
export interface Profile {
  id: string;
  name: string;
  description?: string;
  icon?: string; // アイコンのemoji文字列
  agentApiProxy: AgentApiProxySettings;
  repositoryHistory: RepositoryHistoryItem[];
  environmentVariables: EnvironmentVariable[];
  isDefault: boolean;
  created_at: string;
  updated_at: string;
}

export interface ProfileListItem {
  id: string;
  name: string;
  description?: string;
  icon?: string;
  isDefault: boolean;
  lastUsed?: string;
  repositoryCount: number;
}
```

### ストレージ戦略

既存の localStorage ベースの設定管理を拡張：

1. **プロファイル一覧**: `agentapi-profiles-list`
2. **個別プロファイル**: `agentapi-profile-${profileId}`
3. **デフォルトプロファイル**: `agentapi-default-profile-id`

### プロファイル管理クラス

```typescript
export class ProfileManager {
  // プロファイル一覧の取得
  static getProfiles(): ProfileListItem[]
  
  // プロファイルの詳細取得
  static getProfile(profileId: string): Profile | null
  
  // プロファイルの作成
  static createProfile(profileData: Omit<Profile, 'id' | 'created_at' | 'updated_at'>): Profile
  
  // プロファイルの更新
  static updateProfile(profileId: string, updates: Partial<Profile>): Profile
  
  // プロファイルの削除
  static deleteProfile(profileId: string): boolean
  
  // デフォルトプロファイルの設定
  static setDefaultProfile(profileId: string): void
  
  // デフォルトプロファイルの取得
  static getDefaultProfile(): Profile | null
  
  // プロファイルの使用記録更新
  static markProfileUsed(profileId: string): void
  
  // 設定の移行（既存設定からプロファイルへ）
  static migrateExistingSettings(): void
}
```

## UI設計

### 1. プロファイル管理画面
- **場所**: `/profiles` または設定画面内のタブ
- **機能**:
  - プロファイル一覧の表示（カード形式）
  - 新規プロファイル作成ボタン
  - 各プロファイルの編集・削除・複製ボタン
  - デフォルトプロファイルの設定

### 2. プロファイル作成・編集画面
- **場所**: `/profiles/new`, `/profiles/[id]/edit`
- **フォーム項目**:
  - プロファイル名（必須）
  - 説明文（オプション）
  - アイコン選択（emoji）
  - agentapi proxy 設定（エンドポイント、APIキー、タイムアウト）
  - 環境変数設定
  - デフォルト設定フラグ

### 3. 新規セッション作成時のプロファイル選択
- **場所**: 既存の NewSessionModal の拡張
- **UI要素**:
  - プロファイル選択ドロップダウンまたはカード選択
  - 選択されたプロファイルのプレビュー情報
  - 「プロファイル管理」への遷移リンク

### 4. プロファイル切り替え
- **場所**: TopBar コンポーネント
- **機能**:
  - 現在のプロファイル表示
  - クイックプロファイル切り替えメニュー

## 実装計画

### Phase 1: データ構造とストレージ
1. Profile 関連の型定義作成
2. ProfileManager クラスの実装
3. 既存設定の移行ロジック実装

### Phase 2: プロファイル管理UI
1. プロファイル一覧画面の実装
2. プロファイル作成・編集画面の実装
3. プロファイル削除・複製機能の実装

### Phase 3: セッション作成との統合
1. NewSessionModal の拡張
2. プロファイル選択機能の実装
3. セッション作成時のプロファイル適用

### Phase 4: UX改善
1. TopBar でのプロファイル表示・切り替え
2. リポジトリ履歴の統合表示
3. プロファイルのインポート・エクスポート機能

## 技術的考慮事項

### 1. 既存機能との互換性
- 現在の設定管理（GlobalSettings, RepositorySettings）との共存
- 段階的な移行パスの提供
- 後方互換性の維持

### 2. データの整合性
- プロファイル削除時の参照整合性チェック
- デフォルトプロファイルの存在保証
- 設定データのバリデーション

### 3. パフォーマンス
- localStorage の効率的な使用
- プロファイル一覧の遅延読み込み
- 大量のリポジトリ履歴の処理

### 4. セキュリティ
- APIキーの安全な保存（既存の localStorage ベース）
- プロファイル間でのデータ分離
- エクスポート時の機密情報の取り扱い

## 移行戦略

### 既存ユーザーへの対応
1. 初回アクセス時に既存設定を「Default」プロファイルとして自動変換
2. 既存のリポジトリ履歴をデフォルトプロファイルに移行
3. 移行完了後も既存のキーは保持（後方互換性）

### 段階的な展開
1. プロファイル機能をオプトイン形式で提供
2. ユーザーフィードバックを収集
3. 十分な安定性確認後にデフォルト有効化

## 期待される効果

### 1. 開発効率の向上
- 複数プロジェクト間での迅速な切り替え
- 環境固有の設定の管理
- リポジトリ履歴の統合管理

### 2. ユーザビリティの向上
- 直感的なプロファイル管理
- 設定の再利用とテンプレート化
- チーム間でのプロファイル共有

### 3. スケーラビリティ
- 新しい設定項目の追加容易性
- プロファイル機能の拡張可能性
- エンタープライズ向け機能への発展性