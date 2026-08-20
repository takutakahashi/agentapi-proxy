'use client';

import { useState } from 'react';
import { Question } from '../../types/agentapi';

interface AskUserQuestionModalProps {
  questions: Question[];
  onSubmit: (answers: Record<string, string | string[]>) => void;
  onClose: () => void;
}

export default function AskUserQuestionModal({
  questions,
  onSubmit,
  onClose
}: AskUserQuestionModalProps) {
  const [answers, setAnswers] = useState<Record<string, string | string[]>>({});

  const handleOptionSelect = (questionIndex: number, optionLabel: string, multiSelect: boolean) => {
    const key = questionIndex.toString();

    if (multiSelect) {
      const currentAnswers = (answers[key] as string[] | undefined) || [];
      const newAnswers = currentAnswers.includes(optionLabel)
        ? currentAnswers.filter(a => a !== optionLabel)
        : [...currentAnswers, optionLabel];
      setAnswers({ ...answers, [key]: newAnswers });
    } else {
      setAnswers({ ...answers, [key]: optionLabel });
    }
  };

  const handleSubmit = () => {
    onSubmit(answers);
  };

  const isAnswerSelected = (questionIndex: number, optionLabel: string): boolean => {
    const key = questionIndex.toString();
    const answer = answers[key];

    if (Array.isArray(answer)) {
      return answer.includes(optionLabel);
    }
    return answer === optionLabel;
  };

  const allQuestionsAnswered = questions.every((_, index) => {
    const key = index.toString();
    const answer = answers[key];
    if (Array.isArray(answer)) {
      return answer.length > 0;
    }
    return !!answer;
  });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black bg-opacity-50 p-4">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-2xl w-full max-h-[90vh] overflow-y-auto">
        <div className="p-6">
          <div className="flex justify-between items-start mb-6">
            <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
              Agent Question{questions.length > 1 ? 's' : ''}
            </h2>
            <button
              onClick={onClose}
              className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
            >
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>

          <div className="space-y-6">
            {questions.map((question, questionIndex) => (
              <div key={questionIndex} className="border-b border-gray-200 dark:border-gray-700 pb-6 last:border-0">
                <div className="mb-3">
                  <div className="flex items-center gap-2 mb-2">
                    <span className="inline-flex items-center px-2.5 py-0.5 rounded text-xs font-medium bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200">
                      {question.header}
                    </span>
                    {question.multiSelect && (
                      <span className="text-xs text-gray-500 dark:text-gray-400">
                        (Multiple selection allowed)
                      </span>
                    )}
                  </div>
                  <p className="text-base font-medium text-gray-900 dark:text-white">
                    {question.question}
                  </p>
                </div>

                <div className="space-y-2">
                  {question.options.map((option, optionIndex) => {
                    const isSelected = isAnswerSelected(questionIndex, option.label);

                    return (
                      <button
                        key={optionIndex}
                        onClick={() => handleOptionSelect(questionIndex, option.label, question.multiSelect)}
                        className={`w-full text-left p-4 rounded-lg border-2 transition-all ${
                          isSelected
                            ? 'border-purple-500 bg-purple-50 dark:bg-purple-900/20'
                            : 'border-gray-200 dark:border-gray-700 hover:border-gray-300 dark:hover:border-gray-600'
                        }`}
                      >
                        <div className="flex items-start gap-3">
                          <div className="flex-shrink-0 mt-0.5">
                            {question.multiSelect ? (
                              <div className={`w-5 h-5 rounded border-2 flex items-center justify-center ${
                                isSelected
                                  ? 'bg-purple-500 border-purple-500'
                                  : 'border-gray-300 dark:border-gray-600'
                              }`}>
                                {isSelected && (
                                  <svg className="w-3 h-3 text-white" fill="currentColor" viewBox="0 0 20 20">
                                    <path fillRule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clipRule="evenodd" />
                                  </svg>
                                )}
                              </div>
                            ) : (
                              <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center ${
                                isSelected
                                  ? 'border-purple-500'
                                  : 'border-gray-300 dark:border-gray-600'
                              }`}>
                                {isSelected && (
                                  <div className="w-3 h-3 rounded-full bg-purple-500" />
                                )}
                              </div>
                            )}
                          </div>
                          <div className="flex-1">
                            <div className="font-medium text-gray-900 dark:text-white mb-1">
                              {option.label}
                            </div>
                            <div className="text-sm text-gray-600 dark:text-gray-400">
                              {option.description}
                            </div>
                          </div>
                        </div>
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>

          <div className="mt-6 flex justify-end gap-3">
            <button
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md"
            >
              Cancel
            </button>
            <button
              onClick={handleSubmit}
              disabled={!allQuestionsAnswered}
              className={`px-4 py-2 text-sm font-medium text-white rounded-md ${
                allQuestionsAnswered
                  ? 'bg-purple-600 hover:bg-purple-700'
                  : 'bg-gray-300 dark:bg-gray-600 cursor-not-allowed'
              }`}
            >
              Submit Answers
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
