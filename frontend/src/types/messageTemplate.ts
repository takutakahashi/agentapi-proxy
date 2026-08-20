export interface MessageTemplate {
  id: string;
  profileId: string;
  name: string;
  content: string;
  createdAt: string;
  updatedAt: string;
}

export interface MessageTemplateInput {
  name: string;
  content: string;
}