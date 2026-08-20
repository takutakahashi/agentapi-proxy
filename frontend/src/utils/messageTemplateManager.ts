import { MessageTemplate, MessageTemplateInput } from '../types/messageTemplate';
import { loadFullGlobalSettings, saveFullGlobalSettings } from '../types/settings';

export class MessageTemplateManager {
  private static instance: MessageTemplateManager;

  private constructor() {}

  static getInstance(): MessageTemplateManager {
    if (!MessageTemplateManager.instance) {
      MessageTemplateManager.instance = new MessageTemplateManager();
    }
    return MessageTemplateManager.instance;
  }

  async getTemplates(): Promise<MessageTemplate[]> {
    const settings = loadFullGlobalSettings();
    return settings.messageTemplates || [];
  }

  async createTemplate(input: MessageTemplateInput): Promise<MessageTemplate> {
    const settings = loadFullGlobalSettings();

    const newTemplate: MessageTemplate = {
      id: this.generateId(),
      profileId: 'global', // Keep for backward compatibility
      name: input.name,
      content: input.content,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    if (!settings.messageTemplates) {
      settings.messageTemplates = [];
    }
    settings.messageTemplates.push(newTemplate);

    saveFullGlobalSettings(settings);

    return newTemplate;
  }

  async updateTemplate(templateId: string, input: Partial<MessageTemplateInput>): Promise<MessageTemplate> {
    const settings = loadFullGlobalSettings();

    if (!settings.messageTemplates) {
      throw new Error('No templates found');
    }

    const templateIndex = settings.messageTemplates.findIndex(t => t.id === templateId);
    if (templateIndex === -1) {
      throw new Error('Template not found');
    }

    const updatedTemplate: MessageTemplate = {
      ...settings.messageTemplates[templateIndex],
      ...(input.name && { name: input.name }),
      ...(input.content && { content: input.content }),
      updatedAt: new Date().toISOString(),
    };

    settings.messageTemplates[templateIndex] = updatedTemplate;

    saveFullGlobalSettings(settings);

    return updatedTemplate;
  }

  async deleteTemplate(templateId: string): Promise<void> {
    const settings = loadFullGlobalSettings();

    if (!settings.messageTemplates) {
      return;
    }

    settings.messageTemplates = settings.messageTemplates.filter(t => t.id !== templateId);

    saveFullGlobalSettings(settings);
  }

  private generateId(): string {
    return `template_${Date.now()}_${Math.random().toString(36).substring(2, 9)}`;
  }
}

export const messageTemplateManager = MessageTemplateManager.getInstance();
