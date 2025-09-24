# This is a modified version of the default robbyrussell theme that includes the
# pgedge_cp_env_prompt_info function.

PROMPT="%(?:%{$fg_bold[green]%}%1{➜%} :%{$fg_bold[red]%}%1{➜%} ) %{$fg[cyan]%}%c%{$reset_color%}"
PROMPT+='$(pgedge_cp_env_prompt_info)'
PROMPT+=' $(git_prompt_info)'

ZSH_THEME_PGEDGE_CP_ENV_PROMPT_PREFIX=" %{$fg_bold[blue]%}cp-env:("
ZSH_THEME_PGEDGE_CP_ENV_PROMPT_SUFFIX=")%{$reset_color%}"

ZSH_THEME_GIT_PROMPT_PREFIX="%{$fg_bold[blue]%}git:(%{$fg[red]%}"
ZSH_THEME_GIT_PROMPT_SUFFIX="%{$reset_color%} "
ZSH_THEME_GIT_PROMPT_DIRTY="%{$fg[blue]%}) %{$fg[yellow]%}%1{✗%}"
ZSH_THEME_GIT_PROMPT_CLEAN="%{$fg[blue]%})"
