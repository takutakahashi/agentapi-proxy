'use client';

import React, { createContext, useContext, useState, useEffect, useCallback, ReactNode } from 'react';
import { ResourceScope } from '../types/agentapi';

interface TeamScopeContextType {
  // Currently selected team (null means personal/user scope)
  selectedTeam: string | null;
  // Available teams for the user
  availableTeams: string[];
  // Set available teams (called when user info is loaded)
  setAvailableTeams: (teams: string[]) => void;
  // Select a team (null for personal scope)
  selectTeam: (team: string | null) => void;
  // Get scope and team_id for API calls
  getScopeParams: () => { scope: ResourceScope; team_id?: string };
  // Check if team scope is active
  isTeamScope: boolean;
  // Loading state
  isLoading: boolean;
}

const STORAGE_KEY = 'agentapi_selected_team';

const TeamScopeContext = createContext<TeamScopeContextType | undefined>(undefined);

export const useTeamScope = () => {
  const context = useContext(TeamScopeContext);
  if (!context) {
    throw new Error('useTeamScope must be used within a TeamScopeProvider');
  }
  return context;
};

interface TeamScopeProviderProps {
  children: ReactNode;
}

export const TeamScopeProvider: React.FC<TeamScopeProviderProps> = ({ children }) => {
  const [selectedTeam, setSelectedTeam] = useState<string | null>(null);
  const [availableTeams, setAvailableTeamsState] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  // Load saved team selection from localStorage on mount
  useEffect(() => {
    if (typeof window !== 'undefined') {
      try {
        const saved = localStorage.getItem(STORAGE_KEY);
        if (saved) {
          setSelectedTeam(saved);
        }
      } catch (error) {
        console.error('Failed to load team selection from storage:', error);
      }
      setIsLoading(false);
    }
  }, []);

  // Validate selected team when available teams change
  useEffect(() => {
    if (selectedTeam && availableTeams.length > 0) {
      // If the selected team is no longer available, reset to personal scope
      if (!availableTeams.includes(selectedTeam)) {
        setSelectedTeam(null);
        if (typeof window !== 'undefined') {
          localStorage.removeItem(STORAGE_KEY);
        }
      }
    }
  }, [selectedTeam, availableTeams]);

  const setAvailableTeams = useCallback((teams: string[]) => {
    setAvailableTeamsState(teams);
  }, []);

  const selectTeam = useCallback((team: string | null) => {
    setSelectedTeam(team);
    if (typeof window !== 'undefined') {
      try {
        if (team) {
          localStorage.setItem(STORAGE_KEY, team);
        } else {
          localStorage.removeItem(STORAGE_KEY);
        }
      } catch (error) {
        console.error('Failed to save team selection to storage:', error);
      }
    }
  }, []);

  const getScopeParams = useCallback((): { scope: ResourceScope; team_id?: string } => {
    if (selectedTeam) {
      return { scope: 'team', team_id: selectedTeam };
    }
    return { scope: 'user' };
  }, [selectedTeam]);

  const isTeamScope = selectedTeam !== null;

  const value: TeamScopeContextType = {
    selectedTeam,
    availableTeams,
    setAvailableTeams,
    selectTeam,
    getScopeParams,
    isTeamScope,
    isLoading,
  };

  return (
    <TeamScopeContext.Provider value={value}>
      {children}
    </TeamScopeContext.Provider>
  );
};
