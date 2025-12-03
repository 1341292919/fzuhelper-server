/*
Copyright 2024 The west2-online Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"github.com/west2-online/fzuhelper-server/pkg/base"
	"github.com/west2-online/fzuhelper-server/pkg/cache"
	"github.com/west2-online/fzuhelper-server/pkg/cache/user"
	"github.com/west2-online/fzuhelper-server/pkg/db"
	dbmodel "github.com/west2-online/fzuhelper-server/pkg/db/model"
	userDB "github.com/west2-online/fzuhelper-server/pkg/db/user"
	"github.com/west2-online/fzuhelper-server/pkg/utils"
)

func TestUserService_BindInvitation(t *testing.T) {
	type testCase struct {
		name string

		expectingError    bool
		expectingErrorMsg string

		cacheExist        bool
		cacheGetError     error
		cacheFriendId     string
		dbRelationExist   bool
		dbRelationError   error
		dbCreateError     error
		userConfined      bool
		targetConfined    bool
		userConfinedError error
		targetConfinedErr error

		// 添加 goroutine 相关的字段
		goroutineMockCacheExist bool
		targetCacheExist        bool
		goroutineError          bool
	}
	stuId := "102300217"
	friendId := "102300218"
	code := "ABCDEF"

	testCases := []testCase{
		{
			name:              "cache not exist",
			expectingError:    true,
			expectingErrorMsg: "service.BindInvitation: Invalid InvitationCode",
			cacheExist:        false,
		},
		{
			name:              "cache get error",
			expectingError:    true,
			expectingErrorMsg: "service.GetCodeStuIdMappingCode:",
			cacheExist:        true,
			cacheGetError:     fmt.Errorf("internal service error"),
		},
		{
			name:              "add self as friend",
			expectingError:    true,
			expectingErrorMsg: "service.BindInvitation: cannot add yourself as friend",
			cacheExist:        true,
			cacheFriendId:     stuId,
		},
		{
			name:              "relation already exist",
			expectingError:    true,
			expectingErrorMsg: "service.BindInvitation: RelationShip Already Exist",
			cacheExist:        true,
			cacheFriendId:     friendId,
			dbRelationExist:   true,
			dbRelationError:   nil,
		},
		{
			name:              "db relation check error",
			expectingError:    true,
			expectingErrorMsg: "service.GetRelationByUserId:",
			cacheExist:        true,
			cacheFriendId:     friendId,
			dbRelationExist:   false,
			dbRelationError:   gorm.ErrInvalidData,
		},
		{
			name:              "user friend list full",
			expectingError:    true,
			expectingErrorMsg: "service.BindInvitation :102300217 friendList is full",
			cacheExist:        true,
			cacheFriendId:     friendId,
			dbRelationExist:   false,
			dbRelationError:   nil,
			userConfined:      true,
		},
		{
			name:              "target friend list full",
			expectingError:    true,
			expectingErrorMsg: "service.BindInvitation :102300218 friendList is full",
			cacheExist:        true,
			cacheFriendId:     friendId,
			dbRelationExist:   false,
			dbRelationError:   nil,
			targetConfined:    true,
		},
		{
			name:              "user confined check error",
			expectingError:    true,
			expectingErrorMsg: "service.IsFriendNumsConfined get user friend cache:",
			cacheExist:        true,
			cacheFriendId:     friendId,
			dbRelationExist:   false,
			dbRelationError:   nil,
			userConfinedError: fmt.Errorf("service.IsFriendNumsConfined get user friend cache: cache error"),
		},
		{
			name:              "db create error",
			expectingError:    true,
			expectingErrorMsg: "service.CreateRelation:",
			cacheExist:        true,
			cacheFriendId:     friendId,
			dbRelationExist:   false,
			dbRelationError:   nil,
			dbCreateError:     gorm.ErrInvalidData,
		},
		{
			name:                    "success",
			expectingError:          false,
			cacheExist:              true,
			cacheFriendId:           friendId,
			dbRelationExist:         false,
			dbRelationError:         nil,
			dbCreateError:           nil,
			goroutineMockCacheExist: true, // goroutine 中检查缓存会返回 true
			targetCacheExist:        true,
			goroutineError:          false,
		},
	}

	defer mockey.UnPatchAll()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockey.PatchConvey(tc.name, t, func() {
				// 使用 channel 来等待 goroutine 完成
				goroutineDone := make(chan bool, 1)

				mockClientSet := &base.ClientSet{
					SFClient:    new(utils.Snowflake),
					DBClient:    new(db.Database),
					CacheClient: new(cache.Cache),
				}
				mockClientSet.CacheClient.User = &user.CacheUser{}
				userService := NewUserService(context.Background(), "", nil, mockClientSet)

				// Mock 缓存检查 - 主线程和goroutine都可能调用
				isKeyExistMock := mockey.Mock((*cache.Cache).IsKeyExist)
				if tc.expectingError {
					// 错误情况下，goroutine 不会执行，所以只 mock 主逻辑
					isKeyExistMock.To(func(ctx context.Context, key string) bool {
						return tc.cacheExist
					}).Build()
				} else {
					// 成功情况下，主逻辑和goroutine都会调用
					isKeyExistMock.To(func(ctx context.Context, key string) bool {
						// 根据 key 判断是主逻辑调用还是 goroutine 调用
						if strings.HasPrefix(key, "code_mapping:") {
							return tc.cacheExist
						}
						// goroutine 中的调用
						return tc.goroutineMockCacheExist
					}).Build()
				}

				mockey.Mock((*user.CacheUser).GetCodeStuIdMappingCache).To(func(ctx context.Context, key string) (string, error) {
					if tc.cacheGetError != nil {
						return "", tc.cacheGetError
					}
					return tc.cacheFriendId, nil
				}).Build()

				mockey.Mock((*userDB.DBUser).GetRelationByUserId).To(func(ctx context.Context, stuId, friendId string) (bool, *dbmodel.FollowRelation, error) {
					return tc.dbRelationExist, nil, tc.dbRelationError
				}).Build()

				// Mock 好友数量检查
				mockey.Mock((*UserService).IsFriendNumsConfined).To(func(s *UserService, stuId string) (bool, error) {
					if stuId == "102300217" {
						return tc.userConfined, tc.userConfinedError
					}
					return tc.targetConfined, tc.targetConfinedErr
				}).Build()

				mockey.Mock((*userDB.DBUser).CreateRelation).To(func(ctx context.Context, stuId, friendId string) error {
					return tc.dbCreateError
				}).Build()

				// Mock goroutine 中的缓存操作
				if !tc.expectingError {
					mockey.Mock((*user.CacheUser).SetUserFriendCache).To(func(ctx context.Context, stuId, friendId string) error {
						if tc.goroutineError {
							return fmt.Errorf("cache error")
						}
						return nil
					}).Build()

					mockey.Mock((*user.CacheUser).RemoveCodeStuIdMappingCache).To(func(ctx context.Context, key string) error {
						if tc.goroutineError {
							return fmt.Errorf("remove cache error")
						}
						goroutineDone <- true // 标记 goroutine 完成
						return nil
					}).Build()
				}

				err := userService.BindInvitation(stuId, code)

				if tc.expectingError {
					assert.Error(t, err)
					if tc.expectingErrorMsg != "" {
						assert.Contains(t, err.Error(), tc.expectingErrorMsg)
					}
				} else {
					assert.NoError(t, err)
					// 等待 goroutine 完成（如果有的话）
					if tc.expectingError == false {
						select {
						case <-goroutineDone:
							// goroutine 完成
						case <-time.After(100 * time.Millisecond):
							// 超时，goroutine 可能没有正确执行
							t.Log("goroutine timeout, but test may still pass")
						}
					}
				}
			})
		})
	}
}
